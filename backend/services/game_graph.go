package services

import (
	"bytes"
	"fmt"
	"islandBattles/backend/dal"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"gorm.io/gorm"
)

func GetGameGraph(db *gorm.DB, gameID uint) (string, error) {
	game, err := dal.GetGame(db, gameID)
	if err != nil {
		return "", fmt.Errorf("game not found: %w", err)
	}

	page := components.NewPage()
	page.PageTitle = fmt.Sprintf("Game %d Visualization", gameID)

	for i, generation := range game.Generations {
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
				Title: fmt.Sprintf("Game ID: %d - Generation ID: %d (Step %d/%d)", gameID, generation.ID, i+1,
					len(game.Generations)),
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
		page.AddCharts(graph)
	}

	var buffer bytes.Buffer
	if err := page.Render(&buffer); err != nil {
		return "", fmt.Errorf("failed to render graph: %w", err)
	}

	htmlContent := buffer.String()

	// Try to find the template files in multiple possible locations
	possiblePaths := []string{
		"graph",                            // Relative to backend/services/
		filepath.Join("services", "graph"), // Relative to backend/
		filepath.Join("backend", "services", "graph"), // Relative to project root
	}

	var controlsHTML, scriptJS string
	var pathFound bool

	for _, possiblePath := range possiblePaths {
		controlsHTMLBytes, controlsHTMLErr := os.ReadFile(filepath.Join(possiblePath, "controls.html"))
		scriptJSBytes, scriptJSErr := os.ReadFile(filepath.Join(possiblePath, "script.js"))
		if controlsHTMLErr == nil && scriptJSErr == nil {
			controlsHTML = string(controlsHTMLBytes)
			scriptJS = string(scriptJSBytes)
			pathFound = true
			break
		}
	}

	if !pathFound {
		return "", fmt.Errorf("failed to find template files in possible locations: %v", possiblePaths)
	}

	// Inject variable and wrap in script tag
	scriptHTML := fmt.Sprintf("<script type=\"module\">\nwindow.totalGens = %d;\n%s\n</script>", len(game.Generations), scriptJS)

	htmlContent = strings.Replace(htmlContent, "<body>", "<body>"+controlsHTML, 1)

	htmlContent = strings.Replace(htmlContent, "</body>", scriptHTML+"</body>", 1)

	return htmlContent, nil
}
