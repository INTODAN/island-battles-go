package handlers

import (
	"bytes"
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
)

func (env *Env) GetGameGraph(ctx context.Context, input *struct {
	GameID uint `path:"id" doc:"The ID of the game"`
}) (*huma.StreamResponse, error) {
	generation, err := dal.GetLatestGenerationForGame(env.DB, input.GameID)
	if err != nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("Generation not found for game %d", input.GameID))
	}

	// Identify unique team colors and create categories
	teamColorMap := make(map[int]int) // TeamColor -> CategoryIndex
	categories := make([]*opts.GraphCategory, 0)
	for _, castle := range generation.Map.Castles {
		if _, exists := teamColorMap[castle.TeamColor]; !exists {
			teamColorMap[castle.TeamColor] = len(categories)
			categories = append(categories, &opts.GraphCategory{
				Name: fmt.Sprintf("Team %d", castle.TeamColor),
			})
		}
	}

	graph := charts.NewGraph()
	graph.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("Game %d - Generation %d Graph", input.GameID, generation.ID),
		}),
		charts.WithLegendOpts(opts.Legend{Show: opts.Bool(true)}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:      opts.Bool(true),
			Formatter: "Castle ID: {b}<br/>Soldier Amount: {c}",
		}),
	)

	halfWidth := float32(generation.Map.PlaneSizeX) / 2
	halfHeight := float32(generation.Map.PlaneSizeY) / 2

	nodes := make([]opts.GraphNode, 0)
	for _, castle := range generation.Map.Castles {
		nodes = append(nodes, opts.GraphNode{
			Name:       strconv.Itoa(int(castle.ID)),
			Value:      float32(castle.SoldierAmount),
			X:          float32(castle.Position.P.X) + halfWidth,
			Y:          float32(castle.Position.P.Y) + halfHeight,
			SymbolSize: 50,
			Category:   teamColorMap[castle.TeamColor],
		})
	}

	graph.AddSeries("Castles", nodes, []opts.GraphLink{}).
		SetSeriesOptions(
			charts.WithGraphChartOpts(opts.GraphChart{
				Layout:             "none",
				Roam:               opts.Bool(false),
				FocusNodeAdjacency: opts.Bool(true),
				Categories:         categories,
			}),
			charts.WithLabelOpts(opts.Label{
				Show:      opts.Bool(true),
				Formatter: "{c}",
			}),
		)

	var buffer bytes.Buffer
	if err := graph.Render(&buffer); err != nil {
		return nil, huma.Error500InternalServerError("Failed to render graph")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/html")
			_, err := ctx.BodyWriter().Write(buffer.Bytes())
			if err != nil {
				fmt.Printf("error encountered while writing %v", err)
				return
			}
		},
	}, nil
}
