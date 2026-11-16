package main

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// gameLobbyPackages are the lobby's packages that Island Battles may use: the
// signed hooks and game API client, the player token verifier, the lobby's
// domain types that those two speak, such as its snapshots, errors and
// channel names, and the Centrifugo publisher, which games publish their own
// events with.
var gameLobbyPackages = []string{
	"github.com/INTODAN/mpl/gamehooks",
	"github.com/INTODAN/mpl/playertoken",
	"github.com/INTODAN/mpl/domain",
	"github.com/INTODAN/mpl/realtime",
}

// lobbyHosts may use any of the lobby's packages, since none of them serves
// the lobby: the schema migration, which runs the map templates' migrations
// on the lobby's migration runner, and the migrate command that runs it, the
// admin MCP server with the lobby's admin tools, the OpenAPI export, which
// describes the lobby's connection token route that game clients call, and
// the test fixtures that build the lobby's own database beside Island
// Battles'. The backend's hosts serve no lobby: it runs on its own (#232).
var lobbyHosts = []string{
	"islandBattles/backend/schema",
	"islandBattles/backend/cmd/migrate",
	"islandBattles/backend/cmd/admin-mcp",
	"islandBattles/backend/cmd/export-openapi",
	"islandBattles/backend/internal/testutils",
}

type listedPackage struct {
	imports, deps []string
}

// listPackages runs go list -deps on patterns and returns every package of
// the graph, by import path, with its imports and all its dependencies.
func listPackages(t *testing.T, patterns ...string) map[string]listedPackage {
	t.Helper()
	command := exec.Command("go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}|{{join .Imports \",\"}}|{{join .Deps \",\"}}"}, patterns...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	packages := map[string]listedPackage{}
	for line := range strings.Lines(string(output)) {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) != 3 {
			t.Fatalf("go list printed %q", line)
		}
		packages[fields[0]] = listedPackage{imports: strings.Split(fields[1], ","), deps: strings.Split(fields[2], ",")}
	}
	return packages
}

func isLobbyPackage(path string) bool {
	return strings.HasPrefix(path, "github.com/INTODAN/mpl/")
}

// Island Battles talks to the lobby as a separate game would. Outside the
// lobby's hosts, the backend imports only gameLobbyPackages of the lobby, and
// depends on no other lobby package than those and what they import.
func TestIslandBattlesUsesOnlyTheLobbysGamePackages(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	allowed := map[string]bool{}
	for path, listed := range listPackages(t, gameLobbyPackages...) {
		if isLobbyPackage(path) {
			allowed[path] = true
		}
		for _, dependency := range listed.deps {
			if isLobbyPackage(dependency) {
				allowed[dependency] = true
			}
		}
	}
	checked := 0
	for path, listed := range listPackages(t, "islandBattles/backend/...") {
		if path != "islandBattles/backend" && !strings.HasPrefix(path, "islandBattles/backend/") || slices.Contains(lobbyHosts, path) {
			continue
		}
		checked++
		for _, imported := range listed.imports {
			if isLobbyPackage(imported) && !slices.Contains(gameLobbyPackages, imported) {
				t.Errorf("%s imports %s; Island Battles may import only %s", path, imported, strings.Join(gameLobbyPackages, ", "))
			}
		}
		for _, dependency := range listed.deps {
			if isLobbyPackage(dependency) && !allowed[dependency] {
				t.Errorf("%s depends on %s, which none of %s needs", path, dependency, strings.Join(gameLobbyPackages, ", "))
			}
		}
	}
	if checked < 20 {
		t.Fatalf("checked %d packages; go list found too few", checked)
	}
}
