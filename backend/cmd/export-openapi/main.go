// Command export-openapi writes the OpenAPI document that Island Battles player
// clients are generated from, without constructing production infrastructure.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

const (
	apiTitle        = "Island Battles Player API"
	apiVersion      = "1.0.0"
	schemaRefPrefix = "#/components/schemas/"
	apiDescription  = "Operations a player client calls during a started game. After every turn, each roster player's " +
		"Centrifugo channel `player:{player_id}` receives a `GenerationEvent`. Clients reconcile with the generation " +
		"read after subscribing or reconnecting; HTTP state is authoritative."
)

// PlayerOperations are the endpoints a gameplay client uses. Lobby setup has
// its own generated SDK in lobby/api/openapi.json.
var PlayerOperations = []string{"createLobbyConnectionToken", "getIslandLobbyGeneration", "registerGenerationCommand"}

// PlayerEventSchemas are delivered by Centrifugo rather than HTTP responses.
var PlayerEventSchemas = []string{"GenerationEvent"}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("export-openapi", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "write OpenAPI JSON to PATH instead of stdout")
	check := flags.Bool("check", false, "check --output without writing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *check && *output == "" {
		return errors.New("--check requires --output")
	}
	document, err := BuildPlayerOpenAPI()
	if err != nil {
		return fmt.Errorf("export player OpenAPI: %w", err)
	}
	if *check {
		current, err := os.ReadFile(*output)
		if err != nil {
			return fmt.Errorf("check OpenAPI %q: %w", *output, err)
		}
		if !bytes.Equal(current, document) {
			return fmt.Errorf("%s is stale; run go run ./backend/cmd/export-openapi --output %s", *output, *output)
		}
		return nil
	}
	if *output == "" {
		_, err := stdout.Write(document)
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(*output), ".openapi-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(document); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), *output)
}

// BuildPlayerOpenAPI registers the production routes, keeps the player
// operations, and drops schemas they do not reference. No handler is called
// while registering, so nil dependencies are sufficient.
func BuildPlayerOpenAPI() ([]byte, error) {
	config := routes.NewAPIConfig(apiTitle, apiVersion)
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	api := humachi.New(chi.NewRouter(), config)
	routes.SetupLobbyRoutes(api, nil, nil, nil, &handlers.LobbyGame{})
	api.OpenAPI().Info.Description = apiDescription

	raw, err := json.Marshal(api.OpenAPI())
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if err := keepOperations(document, PlayerOperations); err != nil {
		return nil, err
	}
	if err := keepReferencedSchemas(document, PlayerEventSchemas); err != nil {
		return nil, err
	}
	compact, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact, "", "  "); err != nil {
		return nil, err
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), nil
}

func keepOperations(document map[string]any, operationIDs []string) error {
	wanted := map[string]bool{}
	for _, operationID := range operationIDs {
		wanted[operationID] = true
	}
	paths, _ := document["paths"].(map[string]any)
	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method, operation := range methods {
			details, _ := operation.(map[string]any)
			if operationID, _ := details["operationId"].(string); wanted[operationID] {
				delete(wanted, operationID)
				continue
			}
			delete(methods, method)
		}
		if len(methods) == 0 {
			delete(paths, path)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for operationID := range wanted {
			missing = append(missing, operationID)
		}
		sort.Strings(missing)
		return fmt.Errorf("player operations are not registered: %s", strings.Join(missing, ", "))
	}
	return nil
}

func keepReferencedSchemas(document map[string]any, roots []string) error {
	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	pending := append([]string{}, roots...)
	collectReferences(document["paths"], &pending)
	kept := map[string]bool{}
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if kept[name] {
			continue
		}
		schema, exists := schemas[name]
		if !exists {
			return fmt.Errorf("schema %q is not registered", name)
		}
		kept[name] = true
		collectReferences(schema, &pending)
	}
	for name := range schemas {
		if !kept[name] {
			delete(schemas, name)
		}
	}
	return nil
}

func collectReferences(value any, names *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if reference, isString := child.(string); key == "$ref" && isString && strings.HasPrefix(reference, schemaRefPrefix) {
				*names = append(*names, strings.TrimPrefix(reference, schemaRefPrefix))
				continue
			}
			collectReferences(child, names)
		}
	case []any:
		for _, child := range typed {
			collectReferences(child, names)
		}
	}
}
