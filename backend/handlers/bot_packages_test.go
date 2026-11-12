package handlers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/packagestore"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
	"islandBattles/backend/uploadtoken"
	"islandBattles/lobby/domain"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// packageLobbies is a lobby service that knows each lobby's active members.
// Like the lobby's own, it refuses a principal without a session.
type packageLobbies struct {
	mutex   sync.Mutex
	members map[string][]domain.Member
}

func (lobbies *packageLobbies) Snapshot(_ context.Context, principal domain.Principal, scope domain.Scope) (domain.Snapshot, error) {
	lobbies.mutex.Lock()
	defer lobbies.mutex.Unlock()
	if principal.PlayerID == "" || principal.SessionID == "" {
		return domain.Snapshot{}, domain.ErrUnauthenticated
	}
	members := lobbies.members[scope.LobbyID]
	for _, member := range members {
		if member.PlayerID == principal.PlayerID && member.Active {
			return domain.Snapshot{Lobby: domain.Lobby{ID: scope.LobbyID, GameKey: scope.GameKey}, Members: members}, nil
		}
	}
	return domain.Snapshot{}, domain.ErrNotFound
}

func (lobbies *packageLobbies) leave(lobbyID, playerID string) {
	lobbies.mutex.Lock()
	defer lobbies.mutex.Unlock()
	for index := range lobbies.members[lobbyID] {
		if lobbies.members[lobbyID][index].PlayerID == playerID {
			lobbies.members[lobbyID][index].Active = false
		}
	}
}

// sessionAuthentication accepts "Bearer <player ID>" for each player, with a
// session of their own.
type sessionAuthentication struct{ players []string }

func (sessionAuthentication) CreateGuest(context.Context, domain.CreateGuestRequest) (domain.GuestSession, error) {
	return domain.GuestSession{}, domain.ErrUnauthenticated
}

func (authentication sessionAuthentication) Authenticate(_ context.Context, authorization string) (domain.SessionVerification, error) {
	for _, player := range authentication.players {
		if authorization == "Bearer "+player {
			return domain.SessionVerification{Principal: domain.Principal{PlayerID: player, SessionID: "session-" + player}}, nil
		}
	}
	return domain.SessionVerification{}, domain.ErrUnauthenticated
}

type packageWorld struct {
	api       humatest.TestAPI
	store     packagestore.Store
	lobbies   *packageLobbies
	lobbyID   string
	other     string
	alex, sam string
	outsider  string
	signer    *uploadtoken.Signer
	clock     *time.Time
}

func newPackageWorld(t *testing.T) *packageWorld {
	t.Helper()
	backend := s3mem.New()
	if err := backend.CreateBucket("packages"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gofakes3.New(backend, gofakes3.WithLogger(gofakes3.DiscardLog())).Server())
	t.Cleanup(server.Close)
	world := &packageWorld{lobbyID: uuid.NewString(), other: uuid.NewString(), alex: uuid.NewString(), sam: uuid.NewString(), outsider: uuid.NewString()}
	world.store = packagestore.Endpoint(server.URL, "packages", "test", "test")
	world.lobbies = &packageLobbies{members: map[string][]domain.Member{
		world.lobbyID: {{PlayerID: world.alex, DisplayName: "Alex", Active: true}, {PlayerID: world.sam, DisplayName: "Sam", Active: true}},
		world.other:   {{PlayerID: world.outsider, DisplayName: "Olu", Active: true}},
	}}
	signer, err := uploadtoken.New([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	world.signer = signer
	now := time.Now()
	world.clock = &now
	handler := &handlers.BotPackages{Authenticator: sessionAuthentication{[]string{world.alex, world.sam, world.outsider}}, Service: services.BotPackages{
		Database: testutils.InitTestDB(t), Lobbies: world.lobbies, GameKey: "island-battles", Store: world.store, Tokens: signer, Now: func() time.Time { return *world.clock }}}
	_, world.api = humatest.New(t)
	routes.SetupBotPackageRoutes(world.api, handler)
	return world
}

func (world *packageWorld) path(rest string) string {
	return "/game/lobbies/" + world.lobbyID + "/bot-packages" + rest
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func detail(t *testing.T, response *httptest.ResponseRecorder, status int) string {
	t.Helper()
	return decode[struct {
		Detail string `json:"detail"`
	}](t, response, status).Detail
}

// zipFile is a file of a test package.
type zipFile struct {
	name, data string
	mode       fs.FileMode
}

func packageZip(t *testing.T, files ...zipFile) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(max(file.mode, 0o644))
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(file.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func pythonZip(t *testing.T, entry string) []byte {
	return packageZip(t, zipFile{name: "island-battles-bot.json", data: `{"format": 1, "language": "python", "python": "3.14", "entry": "` + entry + `"}`},
		zipFile{name: "site/bot.py", data: "def play(turn): ...\n"})
}

func nativeZip(t *testing.T) []byte {
	return packageZip(t, zipFile{name: "island-battles-bot.json", data: `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`},
		zipFile{name: "bot", data: "\x7fELF", mode: 0o755})
}

// start begins an upload with the given Authorization credential.
func (world *packageWorld) start(t *testing.T, credential string) models.BotPackageUploadView {
	t.Helper()
	return decode[models.BotPackageUploadView](t, world.api.Post(world.path(""), "Authorization: Bearer "+credential, map[string]any{"file_name": "island-battles-bot.zip"}), 201)
}

// send uploads data with a presigned POST, as the browser and the CLI do.
func send(t *testing.T, upload models.PresignedBotUploadView, data []byte) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range upload.Fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", "island-battles-bot.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	response, err := http.Post(upload.URL, form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		message, _ := io.ReadAll(response.Body)
		t.Fatalf("upload = %d %s", response.StatusCode, message)
	}
}

func (world *packageWorld) complete(credential, packageID string) *httptest.ResponseRecorder {
	return world.api.Post(world.path("/"+packageID+"/complete"), "Authorization: Bearer "+credential)
}

// upload uploads and completes a package.
func (world *packageWorld) upload(t *testing.T, credential string, data []byte) models.BotPackageView {
	t.Helper()
	started := world.start(t, credential)
	send(t, started.Upload, data)
	return decode[models.BotPackageView](t, world.complete(credential, started.PackageID), 200)
}

func (world *packageWorld) list(t *testing.T, player string) []models.BotPackageView {
	t.Helper()
	return decode[models.BotPackagesView](t, world.api.Get(world.path(""), "Authorization: Bearer "+player), 200).Packages
}

func (world *packageWorld) key(packageID string) string {
	return "lobbies/" + world.lobbyID + "/" + packageID + ".zip"
}

func TestAMemberUploadsAPythonPackageThatEveryMemberSees(t *testing.T) {
	world := newPackageWorld(t)
	data := pythonZip(t, "bot:play")
	started := world.start(t, world.alex)
	if started.Upload.URL == "" || started.Upload.Fields["key"] != world.key(started.PackageID) {
		t.Fatalf("upload = %+v", started)
	}
	send(t, started.Upload, data)
	uploaded := decode[models.BotPackageView](t, world.complete(world.alex, started.PackageID), 200)
	if uploaded.PackageID != started.PackageID || uploaded.OwnerPlayerID != world.alex || uploaded.OwnerName != "Alex" || uploaded.Language != "python" ||
		uploaded.EntryOrCommand != "bot:play" || uploaded.SizeBytes != int64(len(data)) || uploaded.UploadedAt.IsZero() {
		t.Fatalf("package = %+v", uploaded)
	}
	listed := world.list(t, world.sam)
	if len(listed) != 1 || listed[0] != uploaded {
		t.Fatalf("Sam sees %+v, want %+v", listed, uploaded)
	}
	// Completing it again returns it.
	if again := decode[models.BotPackageView](t, world.complete(world.alex, started.PackageID), 200); again != uploaded {
		t.Fatalf("again = %+v", again)
	}
}

func TestTheCommandLineUploadsANativePackageWithAnUploadToken(t *testing.T) {
	world := newPackageWorld(t)
	issued := decode[models.BotUploadTokenView](t, world.api.Post(world.path("/upload-token"), "Authorization: Bearer "+world.sam), 200)
	if !strings.HasPrefix(issued.Token, "ibut1.") || !issued.ExpiresAt.Equal(world.clock.Add(15*time.Minute).Truncate(time.Second)) {
		t.Fatalf("token = %+v", issued)
	}
	claims, err := world.signer.Verify(issued.Token, *world.clock)
	if err != nil || claims.LobbyID != world.lobbyID || claims.PlayerID != world.sam || claims.SessionID != "session-"+world.sam {
		t.Fatalf("claims = %+v, %v", claims, err)
	}
	uploaded := world.upload(t, issued.Token, nativeZip(t))
	if uploaded.OwnerPlayerID != world.sam || uploaded.OwnerName != "Sam" || uploaded.Language != "native" || uploaded.EntryOrCommand != "./bot worker" {
		t.Fatalf("package = %+v", uploaded)
	}
	if listed := world.list(t, world.alex); len(listed) != 1 || listed[0].PackageID != uploaded.PackageID {
		t.Fatalf("Alex sees %+v", listed)
	}
}

func TestAnUploadTokenWorksOnlyForItsLobbyItsTimeAndTheUploadEndpoints(t *testing.T) {
	world := newPackageWorld(t)
	token := decode[models.BotUploadTokenView](t, world.api.Post(world.path("/upload-token"), "Authorization: Bearer "+world.alex), 200).Token
	elsewhere := decode[models.BotUploadTokenView](t, world.api.Post("/game/lobbies/"+world.other+"/bot-packages/upload-token", "Authorization: Bearer "+world.outsider), 200).Token
	// Another lobby's token, even its own member's, is 401 here.
	if response := world.api.Post(world.path(""), "Authorization: Bearer "+elsewhere, map[string]any{"file_name": "bot.zip"}); response.Code != 401 {
		t.Fatalf("another lobby's token = %d", response.Code)
	}
	forged := token[:len(token)-3] + "AAA"
	if response := world.api.Post(world.path(""), "Authorization: Bearer "+forged, map[string]any{"file_name": "bot.zip"}); response.Code != 401 {
		t.Fatalf("forged token = %d", response.Code)
	}
	// Only the two upload endpoints take a token.
	for name, response := range map[string]*httptest.ResponseRecorder{
		"list":         world.api.Get(world.path(""), "Authorization: Bearer "+token),
		"delete":       world.api.Delete(world.path("/"+uuid.NewString()), "Authorization: Bearer "+token),
		"upload token": world.api.Post(world.path("/upload-token"), "Authorization: Bearer "+token),
	} {
		if response.Code != 401 {
			t.Errorf("%s with a token = %d", name, response.Code)
		}
	}
	started := world.start(t, token)
	*world.clock = world.clock.Add(15 * time.Minute)
	if response := world.complete(token, started.PackageID); response.Code != 401 {
		t.Fatalf("an expired token = %d", response.Code)
	}
}

func TestOnlyActiveMembersReachTheLobbysPackages(t *testing.T) {
	world := newPackageWorld(t)
	alexToken := decode[models.BotUploadTokenView](t, world.api.Post(world.path("/upload-token"), "Authorization: Bearer "+world.alex), 200).Token
	for name, response := range map[string]*httptest.ResponseRecorder{
		"start":        world.api.Post(world.path(""), "Authorization: Bearer "+world.outsider, map[string]any{"file_name": "bot.zip"}),
		"list":         world.api.Get(world.path(""), "Authorization: Bearer "+world.outsider),
		"upload token": world.api.Post(world.path("/upload-token"), "Authorization: Bearer "+world.outsider),
		"delete":       world.api.Delete(world.path("/"+uuid.NewString()), "Authorization: Bearer "+world.outsider),
		"complete":     world.complete(world.outsider, uuid.NewString()),
	} {
		if response.Code != 404 {
			t.Errorf("%s by someone outside the lobby = %d", name, response.Code)
		}
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"start": world.api.Post(world.path(""), map[string]any{"file_name": "bot.zip"}),
		"list":  world.api.Get(world.path("")),
	} {
		if response.Code != 401 {
			t.Errorf("%s without credentials = %d", name, response.Code)
		}
	}
	// A token outlives nothing: its player must still be in the lobby.
	world.lobbies.leave(world.lobbyID, world.alex)
	if response := world.api.Post(world.path(""), "Authorization: Bearer "+alexToken, map[string]any{"file_name": "bot.zip"}); response.Code != 404 {
		t.Fatalf("a token of a member who left = %d", response.Code)
	}
}

func TestBadPackagesAreRefusedAndDeleted(t *testing.T) {
	world := newPackageWorld(t)
	cases := map[string]struct {
		data []byte
		want string
	}{
		"missing manifest": {packageZip(t, zipFile{name: "bot.py", data: "x"}), "the zip has no island-battles-bot.json at its root"},
		"bad entry":        {pythonZip(t, "bot.play"), `the entry "bot.play" must be module:function`},
		"path traversal":   {packageZip(t, zipFile{name: "island-battles-bot.json", data: `{"format": 1, "language": "native", "command": ["./bot"]}`}, zipFile{name: "bot", data: "x"}, zipFile{name: "../../outside", data: "x"}), "../../outside goes outside the package"},
		"symbolic link":    {packageZip(t, zipFile{name: "island-battles-bot.json", data: `{"format": 1, "language": "native", "command": ["./bot"]}`}, zipFile{name: "bot", data: "/bin/sh", mode: fs.ModeSymlink | 0o777}), "bot is a symbolic link"},
		"not a zip":        {[]byte("plain text"), "the file is not a zip archive"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			started := world.start(t, world.alex)
			send(t, started.Upload, test.data)
			message := detail(t, world.complete(world.alex, started.PackageID), 422)
			if !strings.HasPrefix(message, "the package was refused: ") || !strings.Contains(message, test.want) {
				t.Fatalf("detail = %q, want %q", message, test.want)
			}
			if _, err := world.store.Size(context.Background(), world.key(started.PackageID)); !errors.Is(err, packagestore.ErrNotFound) {
				t.Fatalf("the refused zip is still stored: %v", err)
			}
			if response := world.complete(world.alex, started.PackageID); response.Code != 404 {
				t.Fatalf("completing a refused package again = %d", response.Code)
			}
		})
	}
	if listed := world.list(t, world.sam); len(listed) != 0 {
		t.Fatalf("refused packages are listed: %+v", listed)
	}
}

func TestAnOversizePackageIsRefused(t *testing.T) {
	world := newPackageWorld(t)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	manifest, _ := archive.Create("island-battles-bot.json")
	_, _ = manifest.Write([]byte(`{"format": 1, "language": "native", "command": ["./bot"]}`))
	// The central directory says the program is just over 1 GiB unpacked; a
	// zip bomb is checked from the directory alone, never unpacked.
	bot, _ := archive.CreateRaw(&zip.FileHeader{Name: "bot", Method: zip.Deflate, CompressedSize64: 1, UncompressedSize64: 1<<30 + 1})
	_, _ = bot.Write([]byte{0})
	_ = archive.Close()
	started := world.start(t, world.alex)
	send(t, started.Upload, buffer.Bytes())
	if message := detail(t, world.complete(world.alex, started.PackageID), 422); !strings.Contains(message, "more than 1024.0 MiB unpacked") {
		t.Fatalf("detail = %q", message)
	}
}

func TestCompletingBeforeTheUploadChangesNothing(t *testing.T) {
	world := newPackageWorld(t)
	started := world.start(t, world.alex)
	if message := detail(t, world.complete(world.alex, started.PackageID), 409); !strings.Contains(message, "hasn't been uploaded yet") {
		t.Fatalf("detail = %q", message)
	}
	send(t, started.Upload, nativeZip(t))
	decode[models.BotPackageView](t, world.complete(world.alex, started.PackageID), 200)
}

func TestOnlyTheUploaderCompletesAnUpload(t *testing.T) {
	world := newPackageWorld(t)
	started := world.start(t, world.alex)
	send(t, started.Upload, nativeZip(t))
	if response := world.complete(world.sam, started.PackageID); response.Code != 404 {
		t.Fatalf("Sam completing Alex's upload = %d", response.Code)
	}
}

func TestANewPackageReplacesYourPreviousOne(t *testing.T) {
	world := newPackageWorld(t)
	first := world.upload(t, world.alex, pythonZip(t, "bot:play"))
	sams := world.upload(t, world.sam, nativeZip(t))
	*world.clock = world.clock.Add(time.Minute)
	second := world.upload(t, world.alex, nativeZip(t))
	listed := world.list(t, world.alex)
	if len(listed) != 2 || listed[0].PackageID != second.PackageID || listed[1].PackageID != sams.PackageID {
		t.Fatalf("listed = %+v, want Alex's new package first and Sam's", listed)
	}
	if _, err := world.store.Size(context.Background(), world.key(first.PackageID)); !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("the replaced zip is still stored: %v", err)
	}
	if _, err := world.store.Size(context.Background(), world.key(second.PackageID)); err != nil {
		t.Fatalf("the new zip: %v", err)
	}
}

func TestOnlyTheOwnerDeletesAPackage(t *testing.T) {
	world := newPackageWorld(t)
	uploaded := world.upload(t, world.alex, nativeZip(t))
	if message := detail(t, world.api.Delete(world.path("/"+uploaded.PackageID), "Authorization: Bearer "+world.sam), 403); !strings.Contains(message, "only Alex") {
		t.Fatalf("detail = %q", message)
	}
	if listed := world.list(t, world.sam); len(listed) != 1 {
		t.Fatalf("a refused delete deleted: %+v", listed)
	}
	if response := world.api.Delete(world.path("/"+uploaded.PackageID), "Authorization: Bearer "+world.alex); response.Code != 204 {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	if listed := world.list(t, world.sam); len(listed) != 0 {
		t.Fatalf("listed after delete = %+v", listed)
	}
	if _, err := world.store.Size(context.Background(), world.key(uploaded.PackageID)); !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("the deleted zip is still stored: %v", err)
	}
	if response := world.api.Delete(world.path("/"+uploaded.PackageID), "Authorization: Bearer "+world.alex); response.Code != 404 {
		t.Fatalf("deleting again = %d", response.Code)
	}
}

func TestTheFileNameMustBeAZip(t *testing.T) {
	world := newPackageWorld(t)
	for _, name := range []string{"", "bot.tar.gz", strings.Repeat("a", 252) + ".zip"} {
		if response := world.api.Post(world.path(""), "Authorization: Bearer "+world.alex, map[string]any{"file_name": name}); response.Code != 422 {
			t.Errorf("%q = %d", name, response.Code)
		}
	}
	if response := world.api.Post(world.path(""), "Authorization: Bearer "+world.alex, map[string]any{"file_name": "MY-BOT.ZIP"}); response.Code != 201 {
		t.Errorf("an upper-case .ZIP = %d", response.Code)
	}
}

func TestAServerWithoutStorageAnswers503(t *testing.T) {
	_, api := humatest.New(t)
	player := uuid.NewString()
	routes.SetupBotPackageRoutes(api, &handlers.BotPackages{Authenticator: sessionAuthentication{[]string{player}},
		Service: services.BotPackages{Lobbies: &packageLobbies{}, GameKey: "island-battles"}})
	lobby := "/game/lobbies/" + uuid.NewString() + "/bot-packages"
	for name, response := range map[string]*httptest.ResponseRecorder{
		"start":        api.Post(lobby, "Authorization: Bearer "+player, map[string]any{"file_name": "bot.zip"}),
		"list":         api.Get(lobby, "Authorization: Bearer "+player),
		"upload token": api.Post(lobby+"/upload-token", "Authorization: Bearer "+player),
	} {
		if response.Code != 503 {
			t.Errorf("%s = %d", name, response.Code)
		}
	}
}
