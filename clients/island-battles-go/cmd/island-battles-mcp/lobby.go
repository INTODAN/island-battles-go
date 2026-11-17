package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// gameKey is Island Battles' key in the lobby.
const gameKey = "island-battles"

// lobbyClient reads a lobby's game the way a watcher does: a guest session
// on the lobby, a watch with the lobby's join code, and the game's watcher
// reads with a player token. It never joins the lobby or plays.
type lobbyClient struct {
	lobbyURL, gameURL string
	displayName       string
	http              *http.Client
	logger            *slog.Logger

	mutex          sync.Mutex
	session        string
	sessionExpires time.Time
	token          string
	tokenExpires   time.Time
	// lobbies maps join codes to the lobby IDs the watches returned.
	lobbies map[string]string
}

func newLobbyClient(lobbyURL, gameURL, displayName string, client *http.Client, logger *slog.Logger) *lobbyClient {
	return &lobbyClient{lobbyURL: strings.TrimRight(lobbyURL, "/"), gameURL: strings.TrimRight(gameURL, "/"), displayName: displayName,
		http: client, logger: logger, lobbies: map[string]string{}}
}

// problemError is a request the lobby or the game refused.
type problemError struct {
	status int
	what   string
	detail string
}

func (problem *problemError) Error() string {
	if problem.detail == "" {
		return fmt.Sprintf("%s: HTTP %d", problem.what, problem.status)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", problem.what, problem.status, problem.detail)
}

// do sends a JSON request and decodes a JSON answer into answer.
func (client *lobbyClient) do(ctx context.Context, what, method, target, bearer string, body, answer any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 300 {
		var problem struct {
			Detail string `json:"detail"`
			Code   string `json:"code"`
		}
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		_ = json.Unmarshal(raw, &problem)
		detail := problem.Detail
		if problem.Code != "" && !strings.Contains(detail, problem.Code) {
			detail = strings.TrimSpace(problem.Code + " " + detail)
		}
		return &problemError{status: response.StatusCode, what: what, detail: detail}
	}
	if answer == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(answer)
}

// ensureSession creates a guest session when there is none or it is about to
// expire. The caller holds the mutex.
func (client *lobbyClient) ensureSession(ctx context.Context) error {
	if client.session != "" && time.Until(client.sessionExpires) > time.Minute {
		return nil
	}
	// The new session watches anew, so the old one stops watching while it
	// still can.
	client.unwatchLocked(ctx)
	var session struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := client.do(ctx, "create a guest session on the lobby", http.MethodPost, client.lobbyURL+"/lobby/v1/sessions", "",
		map[string]string{"display_name": client.displayName}, &session); err != nil {
		return err
	}
	client.session, client.sessionExpires = session.AccessToken, session.ExpiresAt
	client.token = ""
	return nil
}

// watch watches the lobby with code and returns its lobby ID.
func (client *lobbyClient) watch(ctx context.Context, code string) (string, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if err := client.ensureSession(ctx); err != nil {
		return "", err
	}
	if lobbyID, found := client.lobbies[code]; found {
		return lobbyID, nil
	}
	var access struct {
		LobbyID string `json:"lobby_id"`
	}
	err := client.do(ctx, "watch the lobby with code "+code, http.MethodPost, client.lobbyURL+"/lobby/v1/watches", client.session,
		map[string]string{"game_key": gameKey, "join_code": code}, &access)
	var problem *problemError
	if errors.As(err, &problem) && problem.status == http.StatusNotFound {
		return "", fmt.Errorf("no Island Battles lobby has the code %s: check the code; a lobby is deleted some time after its game ends", code)
	}
	if err != nil {
		return "", err
	}
	client.lobbies[code] = access.LobbyID
	return access.LobbyID, nil
}

// unwatch stops watching every lobby this client watches, so the lobby's
// host no longer sees it among the watchers, and restarts don't pile up
// watchers toward the lobby's limit. It is best effort: a failure leaves a
// watcher row, which the host can remove.
func (client *lobbyClient) unwatch(ctx context.Context) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.unwatchLocked(ctx)
}

// unwatchLocked is unwatch for a caller that holds the mutex.
func (client *lobbyClient) unwatchLocked(ctx context.Context) {
	if client.session != "" && time.Now().Before(client.sessionExpires) {
		for code, lobbyID := range client.lobbies {
			// An active member of the lobby has no watcher row, and gets 404.
			err := client.do(ctx, "stop watching the lobby with code "+code, http.MethodDelete,
				client.lobbyURL+"/lobby/v1/games/"+gameKey+"/lobbies/"+url.PathEscape(lobbyID)+"/watch", client.session, nil, nil)
			var problem *problemError
			notWatching := errors.As(err, &problem) && problem.status == http.StatusNotFound
			if err != nil && !notWatching && client.logger != nil {
				client.logger.Warn("Couldn't stop watching the lobby; its host can remove the watcher", "code", code, "error", err)
			}
		}
	}
	clear(client.lobbies)
}

// playerToken returns a player token for the game, issuing a new one at
// most a minute before the old one expires.
func (client *lobbyClient) playerToken(ctx context.Context) (string, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if err := client.ensureSession(ctx); err != nil {
		return "", err
	}
	if client.token != "" && time.Until(client.tokenExpires) > time.Minute {
		return client.token, nil
	}
	var token struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := client.do(ctx, "get a player token from the lobby", http.MethodPost, client.lobbyURL+"/lobby/v1/player-tokens", client.session,
		map[string]string{"game_key": gameKey}, &token); err != nil {
		return "", err
	}
	client.token, client.tokenExpires = token.Token, token.ExpiresAt
	return client.token, nil
}

// readGame watches the lobby with code and reads its newest game's history
// in the demo format, with the bot logs a watcher may see: none while the
// game runs, every team's once it has ended.
func (client *lobbyClient) readGame(ctx context.Context, code string) (Demo, error) {
	lobbyID, err := client.watch(ctx, code)
	if err != nil {
		return Demo{}, err
	}
	token, err := client.playerToken(ctx)
	if err != nil {
		return Demo{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.gameURL+"/game/from-lobby/"+url.PathEscape(lobbyID)+"/watch/demo", nil)
	if err != nil {
		return Demo{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	// Go's transport asks for gzip and unpacks the answer itself.
	response, err := client.http.Do(request)
	if err != nil {
		return Demo{}, fmt.Errorf("read the game of lobby %s: %w", code, err)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return Demo{}, fmt.Errorf("the lobby with code %s has no game yet: its host hasn't started it, or the game was deleted", code)
	case response.StatusCode >= 300:
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var problem struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &problem)
		return Demo{}, &problemError{status: response.StatusCode, what: "read the game of lobby " + code, detail: problem.Detail}
	}
	return readDemo(response.Body)
}
