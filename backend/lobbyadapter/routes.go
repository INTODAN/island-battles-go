package lobbyadapter

import (
	"context"
	"encoding/json"
	"net/http"

	"islandBattles/lobby"

	"github.com/danielgtaylor/huma/v2"
)

type bindRequest struct {
	GameKey string `json:"game_key"`
	LobbyID string `json:"lobby_id"`
}

type bindInput struct {
	Body bindRequest
}

type bindOutput struct {
	Body BindingResult
}

type adapterPrincipalKey struct{}

type adapterProblem struct {
	Title  string          `json:"title"`
	Status int             `json:"status"`
	Detail string          `json:"detail"`
	Code   lobby.ErrorCode `json:"code"`
}

func (problem *adapterProblem) Error() string             { return problem.Detail }
func (problem *adapterProblem) GetStatus() int            { return problem.Status }
func (problem *adapterProblem) ContentType(string) string { return "application/problem+json" }

// Register installs the Island Battles-only explicit binding operation. It is
// intentionally outside lobby/httpapi and its generated generic SDK surface.
func Register(api huma.API, service *Service, authenticator lobby.Authenticator) {
	group := huma.NewGroup(api)
	group.UseMiddleware(adapterAuthenticate(authenticator))
	huma.Register(group, huma.Operation{
		OperationID:   "createIslandGameFromLobby",
		Method:        http.MethodPost,
		Path:          "/game/from-lobby",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"Island Battles"},
		Summary:       "Create or return an Island Battles game from a started lobby",
		Description:   "An active host explicitly binds a started island-battles lobby. Replays return the same game and frozen team mapping.",
		Security:      []map[string][]string{{"LobbyBearer": {}}},
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *bindInput) (*bindOutput, error) {
		principal, _ := ctx.Value(adapterPrincipalKey{}).(lobby.Principal)
		result, err := service.Bind(ctx, principal, lobby.Scope{GameKey: input.Body.GameKey, LobbyID: input.Body.LobbyID})
		if err != nil {
			return nil, adapterProblemFor(err)
		}
		return &bindOutput{Body: result}, nil
	})
}

func adapterAuthenticate(authenticator lobby.Authenticator) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if authenticator == nil {
			writeAdapterProblem(ctx, &adapterProblem{Title: "Unauthorized", Status: http.StatusUnauthorized, Detail: "authentication required", Code: lobby.CodeUnauthenticated})
			return
		}
		verified, err := authenticator.Authenticate(ctx.Context(), ctx.Header("Authorization"))
		if err != nil {
			writeAdapterProblem(ctx, adapterProblemFor(err))
			return
		}
		next(huma.WithValue(ctx, adapterPrincipalKey{}, verified.Principal))
	}
}

func adapterProblemFor(err error) *adapterProblem {
	code := lobby.ErrorCodeOf(err)
	status, detail := http.StatusInternalServerError, "internal server error"
	switch code {
	case lobby.CodeUnauthenticated:
		status, detail = http.StatusUnauthorized, "authentication required"
	case lobby.CodeNotFound:
		status, detail = http.StatusNotFound, "lobby not found"
	case lobby.CodeNotHost:
		status, detail = http.StatusForbidden, "host required"
	case lobby.CodePhaseConflict:
		status, detail = http.StatusConflict, err.Error()
	case lobby.CodeUnavailable:
		status, detail = http.StatusServiceUnavailable, "service temporarily unavailable"
	default:
		code = "internal_error"
	}
	return &adapterProblem{Title: http.StatusText(status), Status: status, Detail: detail, Code: code}
}

func writeAdapterProblem(ctx huma.Context, problem *adapterProblem) {
	ctx.SetStatus(problem.Status)
	ctx.SetHeader("Content-Type", problem.ContentType(""))
	_ = json.NewEncoder(ctx.BodyWriter()).Encode(problem)
}
