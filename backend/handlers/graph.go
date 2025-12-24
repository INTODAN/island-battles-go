package handlers

import (
	"bytes"
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
)

func (env *Env) GetGameGraph(ctx context.Context, input *struct {
	GameID uint `path:"id" doc:"The ID of the game"`
}) (*huma.StreamResponse, error) {
	generations, err := dal.GetGenerationsForGame(env.DB, input.GameID)
	if err != nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("Generations not found for game %d", input.GameID))
	}

	page := components.NewPage()
	page.PageTitle = fmt.Sprintf("Game %d Visualization", input.GameID)

	for i, generation := range generations {
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
			charts.WithInitializationOpts(opts.Initialization{
				ChartID: fmt.Sprintf("gen_%d", i),
			}),
			charts.WithTitleOpts(opts.Title{
				Title: fmt.Sprintf("Game ID: %d - Generation ID: %d (Step %d/%d)", input.GameID, generation.ID, i+1,
					len(generations)),
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
					Roam:               opts.Bool(true),
					FocusNodeAdjacency: opts.Bool(true),
					Categories:         categories,
				}),
				charts.WithLabelOpts(opts.Label{
					Show:      opts.Bool(true),
					Formatter: "{c}",
				}),
			)
		page.AddCharts(graph)
	}

	var buffer bytes.Buffer
	if err := page.Render(&buffer); err != nil {
		return nil, huma.Error500InternalServerError("Failed to render graph")
	}

	htmlContent := buffer.String()

	// Inject Pagination Controls and Logic
	controlsHTML := `
	<div style="text-align: center; margin: 20px;">
		<button onclick="changeGen(-1)" style="padding: 10px 20px; font-size: 16px;">Previous</button>
		<span id="gen-display" style="margin: 0 15px; font-size: 18px; font-weight: bold;">Step 1</span>
		<button onclick="changeGen(1)" style="padding: 10px 20px; font-size: 16px;">Next</button>
	</div>
	`
	scriptHTML := fmt.Sprintf(`
	<script>
		let currentGen = 0;
		const totalGens = %d;

		function showGen(index) {
			// Hide all
			for (let i = 0; i < totalGens; i++) {
				const el = document.getElementById('gen_' + i);
				if (el) el.style.display = 'none';
			}
			// Show current
			const current = document.getElementById('gen_' + index);
			if (current) current.style.display = 'block';

			// Update label
			document.getElementById('gen-display').innerText = 'Step ' + (index + 1) + ' / ' + totalGens;

			// Trigger resize to fix echarts rendering if hidden
			const chartInstance = echarts.getInstanceByDom(current);
			if (chartInstance) chartInstance.resize();
		}

		function changeGen(delta) {
			let newGen = currentGen + delta;
			if (newGen < 0) newGen = 0;
			if (newGen >= totalGens) newGen = totalGens - 1;
			currentGen = newGen;
			showGen(currentGen);
		}

		// Init
		document.addEventListener("DOMContentLoaded", function() {
			showGen(0);
		});
	</script>
	</body>
	`, len(generations))

	htmlContent = strings.Replace(htmlContent, "<body>", "<body>"+controlsHTML, 1)
	htmlContent = strings.Replace(htmlContent, "</body>", scriptHTML, 1)

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/html")
			_, err := ctx.BodyWriter().Write([]byte(htmlContent))
			if err != nil {
				fmt.Printf("error encountered while writing %v", err)
				return
			}
		},
	}, nil
}
