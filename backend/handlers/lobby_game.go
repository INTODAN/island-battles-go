package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

// LobbyGame serves a started game to its frozen roster without reading the lobby.
type LobbyGame struct {
	Database      *gorm.DB
	Authenticator domain.Authenticator
	Publisher     services.Publisher
	// Retention runs at the end of every game request: turns, which are what
	// adds game data, and the state and replay reads that players poll. Nil
	// deletes nothing. retention.Retention is the real one; it has the same
	// shape as the lobby's runner but deletes only game data.
	Retention domain.RetentionRunner
	// Watchers lets the watcher reads admit the lobby's watchers. Nil admits
	// only roster players.
	Watchers domain.WatchAuthorizer
}

// runRetention runs after the handler has its response, and after a turn has
// committed and pushed its events.
func (handler *LobbyGame) runRetention(ctx context.Context) {
	if handler.Retention != nil {
		handler.Retention.RunIfDue(ctx)
	}
}

func (handler *LobbyGame) Replay(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	replay, err := service.Replay(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	if len(replay.Frames) == 0 {
		return nil, huma.Error404NotFound("game result is not ready")
	}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		observability.RecordError(ctx, fmt.Errorf("render replay: %w", err))
		return nil, huma.Error500InternalServerError("game result could not be rendered")
	}
	return &huma.StreamResponse{Body: func(response huma.Context) {
		response.SetHeader("Content-Type", "text/html; charset=utf-8")
		response.SetHeader("Cache-Control", "no-store")
		_, _ = response.BodyWriter().Write([]byte(html))
	}}, nil
}

func (handler *LobbyGame) Demo(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	demo, err := service.Demo(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return streamDemo(ctx, demo)
}

// WatchDemo serves the game's history to its watchers, in the demo format,
// with the game log only once the game has ended.
func (handler *LobbyGame) WatchDemo(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher, Watchers: handler.Watchers}
	demo, err := service.WatchDemo(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return streamDemo(ctx, demo)
}

type WatchStateResponse struct{ Body models.WatchState }

// WatchGeneration serves the game's current generation to its watchers.
func (handler *LobbyGame) WatchGeneration(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*WatchStateResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher, Watchers: handler.Watchers}
	state, err := service.Watch(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &WatchStateResponse{Body: state}, nil
}

// streamDemo writes a demo as a file download, packed when the client
// accepts gzip.
func streamDemo(ctx context.Context, demo models.Demo) (*huma.StreamResponse, error) {
	if len(demo.Frames) == 0 {
		return nil, huma.Error404NotFound("game is not ready")
	}
	return &huma.StreamResponse{Body: func(response huma.Context) {
		response.SetHeader("Content-Type", "application/json")
		response.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", services.DemoFileName(demo)))
		response.SetHeader("Cache-Control", "no-store")
		response.SetHeader("Vary", "Accept-Encoding")
		// Lambda streams a response's first 6 MB at full speed and the rest
		// at about 2 MB/s, so a long game's demo could outlast the request
		// unpacked. Browsers unpack it as they read it.
		write := services.WriteDemo
		if acceptsGzip(response.Header("Accept-Encoding")) {
			response.SetHeader("Content-Encoding", "gzip")
			write = services.WriteCompressedDemo
		}
		// The status went out with the first bytes, so a failed write can
		// only cut the body short. A player who leaves cancels the request,
		// which is not an error.
		if err := write(response.BodyWriter(), demo); err != nil && ctx.Err() == nil {
			observability.RecordError(ctx, fmt.Errorf("write demo: %w", err))
		}
	}}, nil
}

// acceptsGzip reports whether an Accept-Encoding header lists gzip without
// refusing it with q=0.
func acceptsGzip(acceptEncoding string) bool {
	for coding := range strings.SplitSeq(acceptEncoding, ",") {
		name, parameters, _ := strings.Cut(coding, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		for parameter := range strings.SplitSeq(parameters, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(parameter), "=")
			if strings.EqualFold(key, "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				return err == nil && quality > 0
			}
		}
		return true
	}
	return false
}
