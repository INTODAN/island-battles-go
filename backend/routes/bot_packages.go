package routes

import (
	"net/http"
	"reflect"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
)

// UploadTokenScheme names the security scheme of the upload tokens that the
// bot picker issues for the command line.
const UploadTokenScheme = "UploadToken"

// SetupBotPackageRoutes registers the bot packages that members upload to a
// lobby.
func SetupBotPackageRoutes(api huma.API, handler *handlers.BotPackages) {
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes[UploadTokenScheme] = &huma.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "ibut1",
		Description: "An upload token from POST /game/lobbies/{lobby_id}/bot-packages/upload-token, which starts with ibut1. It stands for its player in POST /game/lobbies/{lobby_id}/bot-packages and its complete, for that lobby only, for 15 minutes. " +
			"Its payload, the base64url JSON between its two dots, names the lobby as lobby_id, which the command line reads."}
	const membership = "Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken); anyone else gets 404. "
	const uploader = "Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken) or an upload token for this lobby (Authorization: Bearer ibut1.…); anyone else gets 404, and an upload token for another lobby, or an expired one, 401. "
	const storage = " A server without storage for packages answers 503."
	uploaders := []map[string][]string{{PlayerTokenScheme: {}}, {UploadTokenScheme: {}}}
	members := playerTokenSecurity
	huma.Register(api, huma.Operation{
		OperationID: "startIslandBotPackageUpload", Path: "/game/lobbies/{lobby_id}/bot-packages", Method: http.MethodPost, DefaultStatus: http.StatusCreated,
		Summary: "Start uploading a bot package to a lobby",
		Description: uploader + "A bot package is the zip that the SDKs' package command builds: island-battles-bot package in Python, or the Go SDK's island-battles-package. A package belongs to the lobby, and anyone in it may upload one. " +
			"This stores a pending package and returns its package_id and a presigned POST: send a multipart/form-data POST to upload.url with every field of upload.fields, unchanged, and the zip last, in a field named file. " +
			"The zip goes straight to storage, never through this server, so it may be up to 250 MiB (262,144,000 bytes); the presigned POST refuses an empty or larger file, and expires after 15 minutes. " +
			"Then call POST /game/lobbies/{lobby_id}/bot-packages/{package_id}/complete. file_name must end in .zip, or this is 422." + storage,
		Tags: []string{"Island Battles"}, Security: uploaders,
		// The service checks file_name itself, after authenticating the
		// caller, so a request without credentials is refused first.
		SkipValidateBody: true,
	}, handler.Start)
	// A request body, not a linked resource.
	dropSchemaLink(api, reflect.TypeFor[models.BotPackageUploadRequest]())
	huma.Register(api, huma.Operation{
		OperationID: "completeIslandBotPackageUpload", Path: "/game/lobbies/{lobby_id}/bot-packages/{package_id}/complete", Method: http.MethodPost,
		Summary: "Check an uploaded bot package and add it to the lobby",
		Description: uploader + "Only the member who started the upload can complete it; for anyone else the package is 404. " +
			"It reads the zip's central directory and island-battles-bot.json with range requests, without downloading the zip, and checks that: the manifest is format 1, either python with python \"3.14\" and a module:function entry, or native with a command whose program is a file in the zip; " +
			"the files come to at most 1 GiB (1,073,741,824 bytes) unpacked; and no path leaves the package (.., absolute paths or symbolic links). " +
			"A package that fails a check is 422 with a detail that says what is wrong, and is deleted with its zip: start another upload. Before the zip is uploaded this is 409, and nothing changes. " +
			"A valid package is the member's package in the lobby from then on, and every member sees it in GET /game/lobbies/{lobby_id}/bot-packages. A member has one package per lobby: completing a new one deletes their previous one. Completing it again returns it." + storage,
		Tags: []string{"Island Battles"}, Security: uploaders,
	}, handler.Complete)
	huma.Register(api, huma.Operation{
		OperationID: "listIslandBotPackages", Path: "/game/lobbies/{lobby_id}/bot-packages", Method: http.MethodGet,
		Summary: "List a lobby's bot packages",
		Description: membership + "Lists every package that a member uploaded and completed, newest first, with who uploaded it, its language, its entry or command and its size. " +
			"A member has at most one. Packages are deleted 8 days after their upload, with their zips." + storage,
		Tags: []string{"Island Battles"}, Security: members,
	}, handler.List)
	huma.Register(api, huma.Operation{
		OperationID: "deleteIslandBotPackage", Path: "/game/lobbies/{lobby_id}/bot-packages/{package_id}", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary:     "Delete your bot package from a lobby",
		Description: membership + "Deletes the package and its zip. Only the member who uploaded it can: anyone else gets 403. A package that the lobby doesn't have is 404." + storage,
		Tags:        []string{"Island Battles"}, Security: members,
	}, handler.Delete)
	huma.Register(api, huma.Operation{
		OperationID: "createIslandBotUploadToken", Path: "/game/lobbies/{lobby_id}/bot-packages/upload-token", Method: http.MethodPost,
		Summary: "Get an upload token for uploading a bot package from the command line",
		Description: membership + "Returns an upload token for you and this lobby, valid for 15 minutes, for a command line that doesn't hold your session: " +
			"island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip, or the Go SDK's island-battles-package upload with the same arguments. " +
			"The two upload endpoints, POST /game/lobbies/{lobby_id}/bot-packages and its complete, accept it as Authorization: Bearer <token> in place of your player token; nothing else does. The command line reads the lobby from the token." + storage,
		Tags: []string{"Island Battles"}, Security: members,
	}, handler.UploadToken)
}
