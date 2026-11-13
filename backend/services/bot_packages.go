package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"islandBattles/backend/botpackage"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/packagestore"
	"islandBattles/backend/uploadtoken"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BotPackageUploadWindow is how long a presigned upload is valid.
const BotPackageUploadWindow = 15 * time.Minute

// BotPackages keeps the bot packages that members upload to an Island
// Battles lobby. A package belongs to one lobby; any active member may
// upload one, and has at most one: completing a new upload replaces their
// previous package. The zip goes straight to the bucket with a presigned
// POST and never through the backend; completing the upload checks it with
// range requests.
type BotPackages struct {
	Database *gorm.DB
	// Lobbies authorizes the caller's membership and reads their name.
	Lobbies domain.SnapshotReader
	// GameKey is the lobby game key of Island Battles.
	GameKey string
	// Store keeps the zips. Nil refuses every request with 503: the host has
	// no bucket.
	Store packagestore.Store
	// Tokens signs upload tokens.
	Tokens *uploadtoken.Signer
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

func (service BotPackages) now() time.Time {
	if service.Now == nil {
		return time.Now()
	}
	return service.Now()
}

// notConfigured answers every request of a host without a bucket.
func (service BotPackages) notConfigured() error {
	if service.Store == nil {
		return reject(503, "this server has no storage for bot packages")
	}
	return nil
}

// member returns the caller's membership of the lobby, which the lobby
// authorizes: an active member, or ErrNotFound.
func (service BotPackages) member(ctx context.Context, principal domain.Principal, lobbyID string) (domain.Member, error) {
	snapshot, err := service.Lobbies.Snapshot(ctx, principal, domain.Scope{GameKey: service.GameKey, LobbyID: lobbyID})
	if err != nil {
		return domain.Member{}, err
	}
	for _, member := range snapshot.Members {
		if member.PlayerID == principal.PlayerID {
			return member, nil
		}
	}
	return domain.Member{}, domain.ErrNotFound
}

// TokenPrincipal returns the player an upload token stands for, for the
// lobby it was issued for only. Any other token is uploadtoken.ErrInvalid.
func (service BotPackages) TokenPrincipal(token, lobbyID string) (domain.Principal, error) {
	if service.Tokens == nil {
		return domain.Principal{}, uploadtoken.ErrInvalid
	}
	claims, err := service.Tokens.Verify(token, service.now())
	if err != nil {
		return domain.Principal{}, err
	}
	if claims.LobbyID != lobbyID {
		return domain.Principal{}, uploadtoken.ErrInvalid
	}
	return domain.Principal{PlayerID: claims.PlayerID, SessionID: claims.SessionID}, nil
}

// IssueUploadToken issues an upload token for the caller and the lobby,
// valid for uploadtoken.Lifetime.
func (service BotPackages) IssueUploadToken(ctx context.Context, principal domain.Principal, lobbyID string) (models.BotUploadTokenView, error) {
	if err := service.notConfigured(); err != nil {
		return models.BotUploadTokenView{}, err
	}
	if service.Tokens == nil {
		return models.BotUploadTokenView{}, reject(503, "this server can't issue upload tokens")
	}
	if _, err := service.member(ctx, principal, lobbyID); err != nil {
		return models.BotUploadTokenView{}, err
	}
	expires := service.now().Add(uploadtoken.Lifetime).Truncate(time.Second)
	token, err := service.Tokens.Issue(uploadtoken.Claims{LobbyID: lobbyID, PlayerID: principal.PlayerID, SessionID: principal.SessionID, Expires: expires})
	if err != nil {
		return models.BotUploadTokenView{}, err
	}
	return models.BotUploadTokenView{Token: token, ExpiresAt: expires.UTC()}, nil
}

// Start stores a pending package of the caller and presigns its upload.
func (service BotPackages) Start(ctx context.Context, principal domain.Principal, lobbyID string, request models.BotPackageUploadRequest) (models.BotPackageUploadView, error) {
	if err := service.notConfigured(); err != nil {
		return models.BotPackageUploadView{}, err
	}
	name := strings.TrimSpace(request.FileName)
	if name == "" || len(name) > 255 || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return models.BotPackageUploadView{}, reject(422, "file_name must be the zip's name, ending in .zip, such as island-battles-bot.zip: upload the zip that the SDK's package command writes")
	}
	member, err := service.member(ctx, principal, lobbyID)
	if err != nil {
		return models.BotPackageUploadView{}, err
	}
	packageID := uuid.NewString()
	key := "lobbies/" + lobbyID + "/" + packageID + ".zip"
	upload, err := service.Store.PresignUpload(ctx, key, botpackage.MaxZipBytes, BotPackageUploadWindow)
	if err != nil {
		return models.BotPackageUploadView{}, err
	}
	record := models.BotPackage{PackageID: packageID, LobbyID: lobbyID, OwnerPlayerID: principal.PlayerID, OwnerName: member.DisplayName,
		Status: models.BotPackagePending, S3Key: key, CreatedAt: service.now()}
	if err := dal.CreateBotPackage(service.Database.WithContext(ctx), record); err != nil {
		return models.BotPackageUploadView{}, err
	}
	return models.BotPackageUploadView{PackageID: packageID, Upload: models.PresignedBotUploadView{URL: upload.URL, Fields: upload.Fields}}, nil
}

// notUploaded answers a completion before the zip is in the bucket.
func notUploaded() error {
	return reject(409, "the zip hasn't been uploaded yet: POST it to upload.url with upload.fields, then complete the upload")
}

// Complete checks the caller's uploaded zip and makes it their package in
// the lobby, replacing their previous one. A zip that fails a check is
// deleted with its package, and refused with 422 and the reason.
func (service BotPackages) Complete(ctx context.Context, principal domain.Principal, lobbyID, packageID string) (models.BotPackageView, error) {
	if err := service.notConfigured(); err != nil {
		return models.BotPackageView{}, err
	}
	member, err := service.member(ctx, principal, lobbyID)
	if err != nil {
		return models.BotPackageView{}, err
	}
	database := service.Database.WithContext(ctx)
	record, err := dal.FindBotPackage(database, lobbyID, packageID)
	if err != nil {
		return models.BotPackageView{}, err
	}
	if record == nil || record.OwnerPlayerID != principal.PlayerID {
		return models.BotPackageView{}, reject(404, "no upload of yours has this package_id in this lobby; start another")
	}
	if record.Status == models.BotPackageReady {
		return record.View(), nil
	}
	size, err := service.Store.Size(ctx, record.S3Key)
	if errors.Is(err, packagestore.ErrNotFound) {
		return models.BotPackageView{}, notUploaded()
	}
	if err != nil {
		return models.BotPackageView{}, err
	}
	built, err := botpackage.Inspect(packagestore.ReaderAt(ctx, service.Store, record.S3Key, size), size)
	var refused *botpackage.Invalid
	switch {
	case errors.As(err, &refused):
		service.discard(ctx, *record)
		return models.BotPackageView{}, reject(422, "the package was refused: "+refused.Reason+".")
	case errors.Is(err, packagestore.ErrNotFound):
		return models.BotPackageView{}, notUploaded()
	case err != nil:
		return models.BotPackageView{}, err
	}
	// As the database keeps it, so the answer and later lists agree.
	uploaded := service.now().UTC().Truncate(time.Microsecond)
	record.OwnerName, record.Language, record.EntryOrCommand, record.SizeBytes, record.UploadedAt = member.DisplayName, built.Language, built.EntryOrCommand, size, &uploaded
	replaced, completed, err := dal.CompleteBotPackage(database, *record)
	if err != nil {
		return models.BotPackageView{}, err
	}
	if !completed {
		// Another request completed or deleted it meanwhile.
		again, err := dal.FindBotPackage(database, lobbyID, packageID)
		if err != nil {
			return models.BotPackageView{}, err
		}
		if again == nil || again.Status != models.BotPackageReady {
			return models.BotPackageView{}, reject(404, "this package was deleted; start another upload")
		}
		return again.View(), nil
	}
	for _, key := range replaced {
		service.deleteObject(ctx, key)
	}
	record.Status = models.BotPackageReady
	return record.View(), nil
}

// discard deletes a refused package and its zip.
func (service BotPackages) discard(ctx context.Context, record models.BotPackage) {
	service.deleteObject(ctx, record.S3Key)
	if _, err := dal.DeleteBotPackage(service.Database.WithContext(ctx), record.LobbyID, record.PackageID); err != nil {
		slog.WarnContext(ctx, "Could not delete a refused bot package; retention deletes it", "package_id", record.PackageID, "error", err)
	}
}

// deleteObject deletes a zip that no package refers to any more. If it
// fails, the bucket's lifecycle rule deletes the zip within 8 days.
func (service BotPackages) deleteObject(ctx context.Context, key string) {
	if err := service.Store.Delete(ctx, key); err != nil {
		slog.WarnContext(ctx, "Could not delete a bot package's zip; the bucket's lifecycle rule deletes it", "key", key, "error", err)
	}
}

// List returns the lobby's packages, newest first, for any active member.
func (service BotPackages) List(ctx context.Context, principal domain.Principal, lobbyID string) (models.BotPackagesView, error) {
	if err := service.notConfigured(); err != nil {
		return models.BotPackagesView{}, err
	}
	if _, err := service.member(ctx, principal, lobbyID); err != nil {
		return models.BotPackagesView{}, err
	}
	records, err := dal.ListBotPackages(service.Database.WithContext(ctx), lobbyID)
	if err != nil {
		return models.BotPackagesView{}, err
	}
	view := models.BotPackagesView{Packages: make([]models.BotPackageView, 0, len(records))}
	for _, record := range records {
		view.Packages = append(view.Packages, record.View())
	}
	return view, nil
}

// Delete deletes the caller's package and its zip. Only its owner may.
func (service BotPackages) Delete(ctx context.Context, principal domain.Principal, lobbyID, packageID string) error {
	if err := service.notConfigured(); err != nil {
		return err
	}
	if _, err := service.member(ctx, principal, lobbyID); err != nil {
		return err
	}
	database := service.Database.WithContext(ctx)
	record, err := dal.FindBotPackage(database, lobbyID, packageID)
	if err != nil {
		return err
	}
	if record == nil {
		return reject(404, "the lobby has no package with this package_id")
	}
	if record.OwnerPlayerID != principal.PlayerID {
		return reject(403, fmt.Sprintf("only %s, who uploaded this package, can delete it", record.OwnerName))
	}
	if _, err := dal.DeleteBotPackage(database, lobbyID, packageID); err != nil {
		return err
	}
	service.deleteObject(ctx, record.S3Key)
	return nil
}
