package lobbyadapter

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/gamehooks"
	"islandBattles/backend/observability"
)

// HooksPath is where the backend serves Island Battles' lobby hooks:
// POST /game/lobby-hooks/<hook>. LOBBY_GAMES names it as the game's
// hooks_url, under the game's public URL.
const HooksPath = "/game/lobby-hooks"

// SecretEnv names the environment variable of the hooks' shared secret on
// the lobby's side, as LobbyGame's secret_env. The backend reads the same
// secret as ISLAND_BATTLES_LOBBY_HOOK_SECRET.
const SecretEnv = "LOBBY_GAME_SECRET_ISLAND_BATTLES"

// lobbyGame is Island Battles' entry of LOBBY_GAMES without its hooks_url.
//
//go:embed lobby_game.json
var lobbyGame []byte

// LobbyGame is Island Battles' entry of the lobby's LOBBY_GAMES, with
// hooksURL, such as https://island-battles.example.org/game/lobby-hooks:
// the key and name, the shared secret's variable SecretEnv, the start rule
// that every team needs a player, and the lobby page's catalog entry. The
// entry's result URL is the lobby replay route of the lobby's round, and its
// data URL prefixes admit the game's reads, such as the logs and the demo,
// and a watcher's. Its modules come from the demo package under /game/demo/:
// the example player, the map choice settings module, and the bot choice room
// module, which calls the lobby's bot package routes under its room API
// prefix. Its links open the demo viewer, /game/demo-viewer/, and the 3D web
// client that the deployment serves at /play/. Its seat names and its example
// player's heading give the generic lobby page Island Battles' words for its
// bots.
//
// frontendOrigin is where browsers reach the game, such as
// https://island-battles.example.org, for a lobby page on another origin:
// the catalog entry's URLs are then absolute URLs there, and frontendOrigin
// is the entry's origin. Empty, they are paths on the lobby page's origin,
// which is the game's while the lobby runs in the backend's process.
func LobbyGame(hooksURL, frontendOrigin string) (json.RawMessage, error) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(lobbyGame, &entry); err != nil {
		return nil, err
	}
	url, err := json.Marshal(hooksURL)
	if err != nil {
		return nil, err
	}
	entry["hooks_url"] = url
	if frontendOrigin != "" {
		if entry["frontend"], err = onOrigin(entry["frontend"], frontendOrigin); err != nil {
			return nil, err
		}
	}
	return json.Marshal(entry)
}

// catalogURLFields are the catalog entry's fields that hold one URL path.
var catalogURLFields = []string{"result_url", "data_url_prefix", "watch_result_url", "watch_data_url_prefix", "settings_module", "room_module", "room_api_prefix"}

// onOrigin puts the paths of a catalog entry on origin, and names origin as
// the entry's origin.
func onOrigin(frontend json.RawMessage, origin string) (json.RawMessage, error) {
	var catalog map[string]any
	if err := json.Unmarshal(frontend, &catalog); err != nil {
		return nil, err
	}
	absolute := func(value any) any {
		if path, ok := value.(string); ok && strings.HasPrefix(path, "/") {
			return origin + path
		}
		return value
	}
	for _, field := range catalogURLFields {
		if value, found := catalog[field]; found {
			catalog[field] = absolute(value)
		}
	}
	if links, ok := catalog["links"].([]any); ok {
		for _, link := range links {
			if fields, ok := link.(map[string]any); ok {
				fields["url"] = absolute(fields["url"])
			}
		}
	}
	if player, ok := catalog["example_player"].(map[string]any); ok {
		player["module"] = absolute(player["module"])
	}
	catalog["origin"] = origin
	return json.Marshal(catalog)
}

// Hooks serves registration's hooks, signed with secret, under HooksPath.
// It refuses a request whose Lobby-Signature doesn't verify with 401. The
// cause of a hook that fails, which its 503 answer leaves out, goes on the
// error line of the request that it serves; in the backend's own process that
// is the lobby request that called the hook.
func Hooks(secret []byte, registration domain.GameRegistration) (http.Handler, error) {
	return gamehooks.NewHandler(secret, recordingFailures(registration))
}

// recordingFailures records the errors of a registration's hooks that the
// player doesn't see: those of the checks that could not run, and of the
// start and the round's end, which answer them as unavailable. A nil hook
// stays nil, so the handler answers it as the lobby's default.
func recordingFailures(registration domain.GameRegistration) domain.GameRegistration {
	if validate := registration.ValidateSettings; validate != nil {
		registration.ValidateSettings = func(ctx context.Context, lobbyID string, settings json.RawMessage, teams []domain.TeamDefinition) error {
			return recordUnavailable(ctx, validate(ctx, lobbyID, settings, teams))
		}
	}
	if validate := registration.ValidateSeat; validate != nil {
		registration.ValidateSeat = func(ctx context.Context, lobbyID string, settings json.RawMessage) (string, error) {
			name, err := validate(ctx, lobbyID, settings)
			return name, recordUnavailable(ctx, err)
		}
	}
	if validate := registration.ValidateMemberSettings; validate != nil {
		registration.ValidateMemberSettings = func(ctx context.Context, lobbyID string, settings json.RawMessage) error {
			return recordUnavailable(ctx, validate(ctx, lobbyID, settings))
		}
	}
	if gate := registration.ReadyGate; gate != nil {
		registration.ReadyGate = func(ctx context.Context, lobbyID string) (domain.ReadyCheck, error) {
			check, err := gate(ctx, lobbyID)
			if err != nil || check == nil {
				observability.RecordError(ctx, err)
				return check, err
			}
			return func(members []domain.ReadyMember, atStart bool) error {
				err := check(members, atStart)
				if err != nil && domain.ErrorCodeOf(err) != domain.CodeGameRequirement {
					observability.RecordError(ctx, err)
				}
				return err
			}, nil
		}
	}
	if gameOver := registration.GameOver; gameOver != nil {
		registration.GameOver = func(ctx context.Context, lobbyID string, round int64) (bool, error) {
			over, err := gameOver(ctx, lobbyID, round)
			observability.RecordError(ctx, err)
			return over, err
		}
	}
	if startGame := registration.StartGame; startGame != nil {
		registration.StartGame = func(ctx context.Context, snapshot domain.Snapshot) (string, error) {
			gameID, err := startGame(ctx, snapshot)
			observability.RecordError(ctx, err)
			return gameID, err
		}
	}
	return registration
}

// recordUnavailable records a validator's error that is not a refusal: the
// check could not run.
func recordUnavailable(ctx context.Context, err error) error {
	if errors.Is(err, domain.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		observability.RecordError(ctx, err)
	}
	return err
}
