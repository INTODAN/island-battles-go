package routes

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"islandBattles/backend/dependencies"

	"github.com/danielgtaylor/huma/v2"
)

// AdminAPIKeyHeader carries the admin API key, ISLAND_BATTLES_ADMIN_API_KEY.
const AdminAPIKeyHeader = "X-Admin-API-Key"

const adminAPIKeyScheme = "AdminAPIKey"

var adminAPIKeySecurity = []map[string][]string{{adminAPIKeyScheme: {}}}

func registerAdminAPIKeyScheme(api huma.API) {
	components := api.OpenAPI().Components
	if components.SecuritySchemes == nil {
		components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	components.SecuritySchemes[adminAPIKeyScheme] = &huma.SecurityScheme{
		Type: "apiKey", In: "header", Name: AdminAPIKeyHeader,
		Description: "The deployment's admin API key, " + dependencies.AdminAPIKeyEnv + ". When it isn't set, admin operations refuse every request with 403.",
	}
}

// requireAdminAPIKey runs before the body is read or validated, so callers
// without the key learn nothing about the operation's input. An empty key
// refuses everything.
// It compares SHA-256 digests, which always have the same length, so the
// comparison's timing reveals nothing about the key's length either.
func requireAdminAPIKey(api huma.API, key string) func(huma.Context, func(huma.Context)) {
	keyDigest := sha256.Sum256([]byte(key))
	return func(ctx huma.Context, next func(huma.Context)) {
		if key == "" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "admin operations are disabled because "+dependencies.AdminAPIKeyEnv+" is not set")
			return
		}
		sentDigest := sha256.Sum256([]byte(ctx.Header(AdminAPIKeyHeader)))
		if subtle.ConstantTimeCompare(sentDigest[:], keyDigest[:]) != 1 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "a valid "+AdminAPIKeyHeader+" header is required")
			return
		}
		next(ctx)
	}
}
