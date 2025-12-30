package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"gorm.io/gorm"
)

func BuildGraphForGeneration(generation *models.Generation, index int, totalGens int) *charts.Graph {
	graph := charts.NewGraph()
	graph.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			ChartID: fmt.Sprintf("gen_%d", index),
		}),
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("Game ID: %d - Generation ID: %d (Step %d/%d)", generation.GameID, generation.ID, index+1,
				totalGens),
			Left: "5%",
		}),
		charts.WithLegendOpts(opts.Legend{
			Show:   opts.Bool(true),
			Bottom: "0%",
			Left:   "center",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:      opts.Bool(true),
			Formatter: "ID: {b}<br/>Soldier Amount: {c}",
		}),
	)

	// Standard ECharts palette to ensure consistent colors across categories
	palette := []string{"#5470c6", "#91cc75", "#fac858", "#ee6666", "#73c0de", "#3ba272", "#fc8452", "#9a60b4", "#ea7ccc"}
	teamColorHex := map[int]string{} // TeamColor -> HexColor

	categoryIndices := map[string]int{}
	var categories []*opts.GraphCategory

	// Helper to ensure category exists and returns its index
	ensureCategory := func(name string, teamColor int) int {
		if idx, ok := categoryIndices[name]; ok {
			return idx
		}

		// Determine color for this team
		color, ok := teamColorHex[teamColor]
		if !ok {
			color = palette[len(teamColorHex)%len(palette)]
			teamColorHex[teamColor] = color
		}

		idx := len(categories)
		categoryIndices[name] = idx
		categories = append(categories, &opts.GraphCategory{
			Name: name,
			ItemStyle: &opts.ItemStyle{
				Color: color,
			},
		})
		return idx
	}

	halfWidth := float32(generation.Map.PlaneSizeX) / 2
	halfHeight := float32(generation.Map.PlaneSizeY) / 2

	var nodes []opts.GraphNode
	for _, castle := range generation.Map.Castles {
		catName := fmt.Sprintf("Castles Team %d", castle.TeamColor)
		catIdx := ensureCategory(catName, int(castle.TeamColor))

		nodes = append(nodes, opts.GraphNode{
			Name:       fmt.Sprintf("castle_%d", castle.LogicalCastleID),
			Value:      float32(castle.SoldierAmount),
			X:          float32(castle.Position.P.X) + halfWidth,
			Y:          float32(castle.Position.P.Y) + halfHeight,
			SymbolSize: 50,
			Category:   catIdx,
		})
	}

	for _, soldierGroup := range generation.SoldierGroups {
		teamColor := soldierGroup.TeamColor
		catName := fmt.Sprintf("SoldierGroup Team %d", teamColor)
		catIdx := ensureCategory(catName, int(teamColor))

		nodes = append(nodes, opts.GraphNode{
			Name:       fmt.Sprintf("soldier_group_%d", soldierGroup.LogicalSoldierGroupID),
			Value:      float32(soldierGroup.SoldierAmount),
			X:          float32(soldierGroup.Position.P.X) + halfWidth,
			Y:          float32(soldierGroup.Position.P.Y) + halfHeight,
			SymbolSize: 50,
			Category:   catIdx,
		})
	}

	graph.AddSeries("Generation", nodes, []opts.GraphLink{}).
		SetSeriesOptions(
			charts.WithGraphChartOpts(opts.GraphChart{
				Layout:             "none",
				Roam:               opts.Bool(false),
				FocusNodeAdjacency: opts.Bool(false),
				Categories:         categories,
			}),
			charts.WithEmphasisOpts(opts.Emphasis{
				Focus: "none",
			}),
			charts.WithLabelOpts(opts.Label{
				Show:      opts.Bool(true),
				Formatter: "{c}",
			}),
		)
	return graph
}

func GetGameGraph(db *gorm.DB, gameID uint) (string, error) {
	game, err := dal.GetGame(db, gameID)
	if err != nil {
		return "", fmt.Errorf("game not found: %w", err)
	}

	totalGens := len(game.Generations)
	if totalGens == 0 {
		return "No generations found", nil
	}

	page := components.NewPage()
	page.PageTitle = fmt.Sprintf("Game %d Visualization", gameID)

	// Collect JSON options for ALL generations (including the first)
	var generationsGraphJSON []interface{}
	for i := 0; i < totalGens; i++ {
		graph := BuildGraphForGeneration(&game.Generations[i], i, totalGens)
		generationsGraphJSON = append(generationsGraphJSON, graph.JSON())
	}

	var buffer bytes.Buffer
	if err := page.Render(&buffer); err != nil {
		return "", fmt.Errorf("failed to render graph: %w", err)
	}

	htmlContent := buffer.String()

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
		return "", fmt.Errorf("failed to find template files")
	}

	rawGenerationsGraphJSON, err := json.Marshal(generationsGraphJSON)
	if err != nil {
		return "", fmt.Errorf("failed to marshal generation options: %w", err)
	}

	// Inject data and scripts
	scriptHTML := fmt.Sprintf(`
<script>
    window.totalGens = %d;
    window.generationsGraphJSON = %s;
</script>
<script type="module">
    %s
</script>`, totalGens, string(rawGenerationsGraphJSON), scriptJS)

	htmlContent = strings.Replace(htmlContent, "<body>", "<body>"+controlsHTML, 1)
	htmlContent = strings.Replace(htmlContent, "</body>", scriptHTML+"</body>", 1)

	return htmlContent, nil
}
