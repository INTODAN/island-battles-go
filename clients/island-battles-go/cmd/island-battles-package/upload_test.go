package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	testLobby   = "6f1c2c9e-7d0a-4a43-9c55-6e3f0a1b2c3d"
	testPackage = "0b8f3f5e-3c1a-4d1e-9b7a-2f6c5d4e3a21"
)

func uploadToken(lobby string) string {
	payload := fmt.Sprintf(`{"lobby_id":%q,"player_id":"p","session_id":"s","exp":2000000000}`, lobby)
	return "ibut1." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".mac"
}

// fakeGame is the game and its storage.
type fakeGame struct {
	mutex    sync.Mutex
	requests []string
	// stored is the multipart body storage received.
	stored       []byte
	storedType   string
	storedLength int64
	complete     func(http.ResponseWriter)
	storage      func(http.ResponseWriter)
	auth         []string
}

func newFakeGame(t *testing.T) (*fakeGame, *httptest.Server) {
	t.Helper()
	game := &fakeGame{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		game.mutex.Lock()
		defer game.mutex.Unlock()
		game.requests = append(game.requests, request.Method+" "+request.URL.Path)
		game.auth = append(game.auth, request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/game/lobbies/" + testLobby + "/bot-packages":
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["file_name"] != "island-battles-bot.zip" {
				http.Error(response, "file_name", http.StatusUnprocessableEntity)
				return
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(response, `{"package_id":%q,"upload":{"url":%q,"fields":{"key":"lobbies/k.zip","policy":"p","x-amz-signature":"s"}}}`, testPackage, server.URL+"/storage")
		case "/storage":
			game.stored, _ = io.ReadAll(request.Body)
			game.storedType, game.storedLength = request.Header.Get("Content-Type"), request.ContentLength
			if game.storage != nil {
				game.storage(response)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		case "/game/lobbies/" + testLobby + "/bot-packages/" + testPackage + "/complete":
			if game.complete != nil {
				game.complete(response)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package_id":%q,"owner_player_id":"p","owner_name":"Alex","language":"native","entry_or_command":"./bot worker","size_bytes":4,"uploaded_at":"2026-11-12T12:00:00Z"}`, testPackage)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	return game, server
}

func zipToUpload(t *testing.T, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "island-battles-bot.zip")
	if err := os.WriteFile(path, bytes.Repeat([]byte("z"), size), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadPresignsStoresAndCompletes(t *testing.T) {
	game, server := newFakeGame(t)
	path := zipToUpload(t, 5000)
	var stdout, stderr bytes.Buffer
	if status := run([]string{"upload", "--game-url", server.URL + "/", "--token", uploadToken(testLobby), path}, &stdout, &stderr); status != 0 {
		t.Fatalf("status %d, stderr %q", status, stderr.String())
	}
	if want := "uploaded " + path + ": native, command ./bot worker, 0.0 MiB, package " + testPackage; !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("stdout %q, want %q", stdout.String(), want)
	}
	want := []string{"POST /game/lobbies/" + testLobby + "/bot-packages", "POST /storage", "POST /game/lobbies/" + testLobby + "/bot-packages/" + testPackage + "/complete"}
	if strings.Join(game.requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests %q", game.requests)
	}
	if game.auth[0] != "Bearer "+uploadToken(testLobby) || game.auth[1] != "" || game.auth[2] != game.auth[0] {
		t.Fatalf("authorization %q: the token goes to the game only", game.auth)
	}
	// S3 needs the length, the fields before the file, and the file named
	// file.
	if game.storedLength != int64(len(game.stored)) {
		t.Fatalf("Content-Length %d for %d bytes", game.storedLength, len(game.stored))
	}
	_, parameters, err := mime.ParseMediaType(game.storedType)
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(bytes.NewReader(game.stored), parameters["boundary"])
	var names []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(part)
		names = append(names, part.FormName())
		if part.FormName() == "file" && len(data) != 5000 {
			t.Fatalf("the file part has %d bytes", len(data))
		}
	}
	if len(names) != 4 || names[3] != "file" {
		t.Fatalf("parts %q, want the three fields and then the file", names)
	}
}

func TestUploadShowsTheGamesReasonForABadPackage(t *testing.T) {
	game, server := newFakeGame(t)
	game.complete = func(response http.ResponseWriter) {
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(response, `{"detail":"the package was refused: the command runs ./server, which is not a file in the zip."}`)
	}
	var stdout, stderr bytes.Buffer
	if status := run([]string{"upload", "--game-url", server.URL, "--token", uploadToken(testLobby), zipToUpload(t, 10)}, &stdout, &stderr); status != 1 {
		t.Fatalf("status %d", status)
	}
	if stderr.String() != "FAIL: the package was refused: the command runs ./server, which is not a file in the zip.\n" {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestUploadShowsStoragesRefusal(t *testing.T) {
	game, server := newFakeGame(t)
	game.storage = func(response http.ResponseWriter) {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(response, "<Error><Code>EntityTooLarge</Code><Message>Your proposed upload exceeds the maximum allowed size</Message></Error>")
	}
	_, err := upload(server.Client(), server.URL, uploadToken(testLobby), zipToUpload(t, 10))
	if err == nil || err.Error() != "storage refused the zip: EntityTooLarge: Your proposed upload exceeds the maximum allowed size" {
		t.Fatalf("err %v", err)
	}
}

func TestUploadChecksTheFileAndTheTokenFirst(t *testing.T) {
	game, server := newFakeGame(t)
	empty := zipToUpload(t, 0)
	large := filepath.Join(t.TempDir(), "large.zip")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxZipBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	for name, test := range map[string]struct {
		url, token, path, want string
	}{
		"missing file":   {server.URL, uploadToken(testLobby), filepath.Join(t.TempDir(), "missing.zip"), "is not a file"},
		"empty file":     {server.URL, uploadToken(testLobby), empty, "is empty"},
		"over 250 MiB":   {server.URL, uploadToken(testLobby), large, "the limit is 250.0 MiB"},
		"not a URL":      {"island-battles.example.org", uploadToken(testLobby), empty, "--game-url must be"},
		"a session":      {server.URL, "session-token", empty, "is not an upload token"},
		"a submit token": {server.URL, "ibst1.e30.mac", empty, "is not an upload token"},
		"no lobby":       {server.URL, "ibut1." + base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".mac", empty, "names no lobby"},
		"damaged":        {server.URL, "ibut1.!!.mac", empty, "is damaged"},
	} {
		if _, err := upload(server.Client(), test.url, test.token, test.path); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err %v, want %q", name, err, test.want)
		}
	}
	if len(game.requests) != 0 {
		t.Fatalf("requests were sent: %q", game.requests)
	}
}

func TestUploadNeedsTheGameURLAndTheToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run([]string{"upload", "--token", "x"}, &stdout, &stderr); status != 2 || !strings.Contains(stderr.String(), "Usage: island-battles-package upload") {
		t.Fatalf("status %d, stderr %q", status, stderr.String())
	}
}
