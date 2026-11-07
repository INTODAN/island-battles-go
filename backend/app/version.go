package app

import "net/http"

// Version is the release the binary was built from, such as v0.1.0. The
// release workflow sets it at build time with
// -ldflags "-X islandBattles/backend/app.Version=v0.1.0" (scripts/aws.sh
// builds with ISLAND_BATTLES_VERSION); other builds report "dev".
var Version = "dev"

// VersionHeader carries Version on /healthz, which keeps answering 204.
const VersionHeader = "X-Island-Battles-Version"

// healthz answers readiness checks with 204 and the binary's version.
func healthz(version string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set(VersionHeader, version)
		response.WriteHeader(http.StatusNoContent)
	}
}
