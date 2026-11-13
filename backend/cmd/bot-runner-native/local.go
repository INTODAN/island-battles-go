package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"islandBattles/backend/botrunner/native"
	"islandBattles/backend/models"
)

// runLocal plays one turn of a local game with a package on this machine,
// through the same Handle as on Lambda: it reads the player's turn delivery
// from the game, serves the zip on 127.0.0.1 and submits to the game.
func runLocal(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bot-runner-native", flag.ContinueOnError)
	flags.SetOutput(stderr)
	local := flags.Bool("local", false, "play one turn of a local game (required)")
	packagePath := flags.String("package", "", "the package's zip, built for this machine")
	gameURL := flags.String("game-url", "http://localhost:8888", "the game's URL")
	lobbyID := flags.String("lobby-id", "", "the lobby whose game to play")
	token := flags.String("token", "", "a player token of a player in the game, whose turn the bot plays")
	limits := models.DefaultBotRunnerLimits
	flags.Float64Var(&limits.LoadSeconds, "load-seconds", limits.LoadSeconds, "the time to load the package")
	flags.Float64Var(&limits.PlaySeconds, "play-seconds", limits.PlaySeconds, "the time to play the turn")
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: bot-runner-native --local --package <zip> --game-url <url> --lobby-id <id> --token=<player token>\n\nPlays the player's current turn with the package, as the runner Lambda does,\nand submits it. The package must run on this machine.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !*local || *packagePath == "" || *lobbyID == "" || *token == "" || flags.NArg() > 0 {
		flags.Usage()
		return 2
	}
	outcome, err := playLocal(context.Background(), *packagePath, strings.TrimSuffix(*gameURL, "/"), *lobbyID, *token, limits, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: %v\n", err)
		return 1
	}
	encoded, _ := json.Marshal(outcome)
	_, _ = fmt.Fprintf(stdout, "%s\n", encoded)
	return 0
}

func playLocal(ctx context.Context, packagePath, gameURL, lobbyID, token string, limits models.BotRunnerLimits, stderr io.Writer) (native.Outcome, error) {
	zipData, err := os.ReadFile(packagePath)
	if err != nil {
		return native.Outcome{}, err
	}
	delivery, err := readDelivery(ctx, gameURL, lobbyID, token)
	if err != nil {
		return native.Outcome{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return native.Outcome{}, err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/zip")
		_, _ = response.Write(zipData)
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	sum := sha256.Sum256(zipData)
	payload, err := json.Marshal(map[string]any{
		"package":  models.BotRunnerPackage{ID: "local-" + hex.EncodeToString(sum[:8]), URL: "http://" + listener.Addr().String() + "/package.zip", SizeBytes: int64(len(zipData))},
		"delivery": delivery,
		"limits":   limits,
	})
	if err != nil {
		return native.Outcome{}, err
	}
	submitHost := os.Getenv("GAME_SUBMIT_HOST")
	if submitHost == "" {
		parsed, err := url.Parse(gameURL)
		if err != nil {
			return native.Outcome{}, err
		}
		submitHost = parsed.Host
	}
	directory, err := os.MkdirTemp("", "bot-runner-native-")
	if err != nil {
		return native.Outcome{}, err
	}
	runner := &native.Runner{Directory: directory, SubmitHost: submitHost, AllowHTTPPackages: true,
		Logger: slog.New(slog.NewTextHandler(stderr, nil))}
	defer runner.Unload()
	return runner.Handle(ctx, payload)
}

// readDelivery reads the player's turn of the lobby's game as a delivery,
// as the backend gives it to its runners.
func readDelivery(ctx context.Context, gameURL, lobbyID, token string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, gameURL+"/game/from-lobby/"+url.PathEscape(lobbyID)+"/turn-delivery", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the turn delivery answered %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}
