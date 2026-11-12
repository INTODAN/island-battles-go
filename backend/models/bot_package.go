package models

import "time"

// BotPackageStatus says whether a package's zip has been checked.
type BotPackageStatus string

const (
	// BotPackagePending is a package whose upload was presigned and not yet
	// completed.
	BotPackagePending BotPackageStatus = "pending"
	// BotPackageReady is a package whose zip was uploaded and checked.
	BotPackageReady BotPackageStatus = "ready"
)

// BotPackage is a bot package uploaded to a lobby: the zip that the SDKs'
// package command builds, kept in the bot package bucket at S3Key. A member
// has at most one ready package per lobby. LobbyID and OwnerPlayerID name the
// lobby's records without a foreign key, so the game never constrains the
// lobby's tables (#118); retention deletes the rows 8 days after they are
// created, as the bucket's lifecycle rule deletes the zips.
type BotPackage struct {
	PackageID     string           `gorm:"column:package_id;type:uuid;primaryKey"`
	LobbyID       string           `gorm:"column:lobby_id;type:uuid;not null;index:idx_island_bot_packages_lobby,priority:1"`
	OwnerPlayerID string           `gorm:"column:owner_player_id;type:uuid;not null;index:idx_island_bot_packages_lobby,priority:2"`
	OwnerName     string           `gorm:"column:owner_name;not null"`
	Status        BotPackageStatus `gorm:"column:status;not null"`
	S3Key         string           `gorm:"column:s3_key;not null"`
	// Language, EntryOrCommand, SizeBytes and UploadedAt are set when the
	// upload is completed.
	Language       string     `gorm:"column:language;not null;default:''"`
	EntryOrCommand string     `gorm:"column:entry_or_command;not null;default:''"`
	SizeBytes      int64      `gorm:"column:size_bytes;not null;default:0"`
	UploadedAt     *time.Time `gorm:"column:uploaded_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null;index"`
}

func (BotPackage) TableName() string { return "island_bot_packages" }

// View is the package as GET /game/lobbies/{lobby_id}/bot-packages lists it.
func (record BotPackage) View() BotPackageView {
	view := BotPackageView{PackageID: record.PackageID, OwnerPlayerID: record.OwnerPlayerID, OwnerName: record.OwnerName,
		Language: record.Language, EntryOrCommand: record.EntryOrCommand, SizeBytes: record.SizeBytes}
	if record.UploadedAt != nil {
		view.UploadedAt = *record.UploadedAt
	}
	return view
}

// BotPackageView is a checked bot package of a lobby.
type BotPackageView struct {
	PackageID      string    `json:"package_id" format:"uuid"`
	OwnerPlayerID  string    `json:"owner_player_id" format:"uuid" doc:"The member who uploaded it. Only they can delete it."`
	OwnerName      string    `json:"owner_name" doc:"Their display name when they uploaded it."`
	Language       string    `json:"language" enum:"python,native" doc:"python: a Python bot for the python3.14 runtime. native: a program for Linux arm64, such as a Go bot."`
	EntryOrCommand string    `json:"entry_or_command" doc:"A python package's module:function entry, such as bot:play, or a native package's command, its arguments joined with spaces, such as ./bot worker."`
	SizeBytes      int64     `json:"size_bytes" minimum:"1" doc:"The zip's size in bytes."`
	UploadedAt     time.Time `json:"uploaded_at"`
}

// BotPackagesView lists a lobby's packages.
type BotPackagesView struct {
	Packages []BotPackageView `json:"packages" doc:"Every checked package of the lobby, newest first, at most one per member."`
}

// BotPackageUploadRequest starts an upload.
type BotPackageUploadRequest struct {
	FileName string `json:"file_name" minLength:"1" maxLength:"255" doc:"The zip's file name, such as island-battles-bot.zip. It must end in .zip."`
}

// BotPackageUploadView says where to upload a package's zip.
type BotPackageUploadView struct {
	PackageID string                 `json:"package_id" format:"uuid"`
	Upload    PresignedBotUploadView `json:"upload"`
}

// PresignedBotUploadView is a presigned POST.
type PresignedBotUploadView struct {
	URL    string            `json:"url" doc:"POST a multipart/form-data body here: every field of fields, then the zip as the last field, named file."`
	Fields map[string]string `json:"fields" doc:"The form fields to send before the file, unchanged."`
}

// BotUploadTokenView is an upload token for the command line.
type BotUploadTokenView struct {
	Token     string    `json:"token" doc:"An upload token, ibut1.…: send it as Authorization: Bearer <token> to the two upload endpoints of this lobby, in place of a session."`
	ExpiresAt time.Time `json:"expires_at"`
}
