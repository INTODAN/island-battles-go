package main

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// gameLobbyPackages are the lobby's packages that Island Battles may use: the
// signed hooks and game API client, the player token verifier, and the
// lobby's domain types that those two speak, such as its snapshots, errors and
// channel names.
var gameLobbyPackages = []string{
	"islandBattles/lobby/gamehooks",
	"islandBattles/lobby/playertoken",
	"islandBattles/lobby/domain",
}

// lobbyHosts build the in-process lobby, which production runs until the
// lobby is deployed on its own, so they may use any of its packages: the app
// that builds it with lobby/app (and, locally, reaches a lobby of its own at
// ISLAND_BATTLES_LOBBY_URL instead), the two hosts that serve the app (the
// local server, this package, and the Lambda),
// the schema migration with the lobby's tables and the migrate command, the
// admin MCP server with the lobby's admin tools, the OpenAPI export, which
// describes the lobby's connection token route that game clients call, and
// the test fixtures that build a database with the lobby's tables.
var lobbyHosts = []string{
	"islandBattles/backend",
	"islandBattles/backend/app",
	"islandBattles/backend/cmd/lambda",
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
	return strings.HasPrefix(path, "islandBattles/lobby/")
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
