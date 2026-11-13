package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// uploadTokenPrefix starts every upload token.
const uploadTokenPrefix = "ibut1."

// uploadClient makes the upload's requests; the zip may take minutes to
// send.
var uploadClient = &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// presignedUpload is the game's answer to starting an upload.
type presignedUpload struct {
	PackageID string `json:"package_id"`
	Upload    struct {
		URL    string            `json:"url"`
		Fields map[string]string `json:"fields"`
	} `json:"upload"`
}

// botPackage is an uploaded and checked package.
type botPackage struct {
	PackageID      string `json:"package_id"`
	Language       string `json:"language"`
	EntryOrCommand string `json:"entry_or_command"`
	SizeBytes      int64  `json:"size_bytes"`
}

// runUpload is the upload subcommand:
//
//	island-battles-package upload --game-url <url> --token=<token> [zip]
//
// The token is the one the bot picker's "Upload from the command line"
// shows, which stands for the player in one lobby for 15 minutes.
func runUpload(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("island-battles-package upload", flag.ContinueOnError)
	flags.SetOutput(stderr)
	gameURL := flags.String("game-url", "", "the game's address, such as https://island-battles.example.org")
	token := flags.String("token", "", "the upload token from the bot picker, valid for 15 minutes")
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: island-battles-package upload --game-url <url> --token=<token> [zip]\n\nUploads the zip, %s by default, to the lobby of the upload token that the\nbot picker's \"Upload from the command line\" shows.\n\nFlags:\n", defaultOutput)
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *gameURL == "" || *token == "" || flags.NArg() > 1 {
		flags.Usage()
		return 2
	}
	path := defaultOutput
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	uploaded, err := upload(uploadClient, *gameURL, *token, path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: %v\n", err)
		return 1
	}
	what := "command"
	if uploaded.Language == "python" {
		what = "entry"
	}
	_, _ = fmt.Fprintf(stdout, "uploaded %s: %s, %s %s, %s, package %s. Everyone in the lobby sees it in the bot picker.\n",
		path, uploaded.Language, what, uploaded.EntryOrCommand, mebibytes(uploaded.SizeBytes), uploaded.PackageID)
	return 0
}

// tokenLobby reads the lobby an upload token is for from its payload, which
// isn't secret.
func tokenLobby(token string) (string, error) {
	parts := strings.Split(token, ".")
	if !strings.HasPrefix(token, uploadTokenPrefix) || len(parts) != 3 {
		return "", errors.New("the token is not an upload token (ibut1.…); copy the command again from the bot picker's Upload from the command line")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	var payload struct {
		LobbyID string `json:"lobby_id"`
	}
	if err != nil || json.Unmarshal(data, &payload) != nil {
		return "", errors.New("the upload token is damaged; copy the command again")
	}
	if payload.LobbyID == "" {
		return "", errors.New("the upload token names no lobby; copy the command again")
	}
	return payload.LobbyID, nil
}

// upload sends the zip at path to the token's lobby: the game presigns the
// upload, the zip goes straight to storage, and the game checks it.
func upload(client *http.Client, gameURL, token, path string) (botPackage, error) {
	parsed, err := url.Parse(gameURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return botPackage{}, fmt.Errorf("--game-url must be the game's address, such as https://island-battles.example.org, not %q", gameURL)
	}
	lobby, err := tokenLobby(token)
	if err != nil {
		return botPackage{}, err
	}
	info, err := os.Stat(path)
	switch {
	case err != nil || !info.Mode().IsRegular():
		return botPackage{}, fmt.Errorf("%s is not a file; build it with island-battles-package", path)
	case info.Size() == 0:
		return botPackage{}, fmt.Errorf("%s is empty", path)
	case info.Size() > maxZipBytes:
		return botPackage{}, fmt.Errorf("%s is %s; the limit is %s", path, mebibytes(info.Size()), mebibytes(maxZipBytes))
	}
	packages := strings.TrimRight(gameURL, "/") + "/game/lobbies/" + url.PathEscape(lobby) + "/bot-packages"
	var started presignedUpload
	body, _ := json.Marshal(map[string]string{"file_name": filepath.Base(path)})
	if err := callGame(client, packages, token, body, http.StatusCreated, &started); err != nil {
		return botPackage{}, fmt.Errorf("the game refused the upload: %w", err)
	}
	if err := store(client, started.Upload.URL, started.Upload.Fields, path, info.Size()); err != nil {
		return botPackage{}, err
	}
	var completed botPackage
	if err := callGame(client, packages+"/"+url.PathEscape(started.PackageID)+"/complete", token, nil, http.StatusOK, &completed); err != nil {
		return botPackage{}, err
	}
	return completed, nil
}

// callGame POSTs body to the game with the token, and decodes the answer
// into result, or returns the game's own words for a refusal.
func callGame(client *http.Client, target, token string, body []byte, status int, result any) error {
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != status {
		var problem struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(data, &problem) == nil && problem.Detail != "" {
			return errors.New(problem.Detail)
		}
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("the game answered something unexpected: %w", err)
	}
	return nil
}

// store POSTs the zip to storage with the presigned fields first and the
// file last, with a Content-Length, which S3 requires.
func store(client *http.Client, target string, fields map[string]string, path string, size int64) error {
	var framing bytes.Buffer
	form := multipart.NewWriter(&framing)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return err
		}
	}
	if _, err := form.CreateFormFile("file", filepath.Base(path)); err != nil {
		return err
	}
	head := bytes.Clone(framing.Bytes())
	framing.Reset()
	if err := form.Close(); err != nil {
		return err
	}
	file, err := os.Open(path) //nolint:gosec // the zip the player names
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	request, err := http.NewRequest(http.MethodPost, target, io.MultiReader(bytes.NewReader(head), file, bytes.NewReader(framing.Bytes())))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(head)) + size + int64(framing.Len())
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("storage didn't take the zip: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 == 2 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return fmt.Errorf("storage refused the zip: %s", storageProblem(data, response.StatusCode))
}

var storageTags = regexp.MustCompile(`<(Code|Message)>([^<]*)</(?:Code|Message)>`)

// storageProblem is S3's own code and message, from its XML.
func storageProblem(data []byte, status int) string {
	var found []string
	for _, match := range storageTags.FindAllSubmatch(data, 2) {
		found = append(found, string(match[2]))
	}
	if len(found) == 0 {
		return fmt.Sprintf("HTTP %d", status)
	}
	return strings.Join(found, ": ")
}
