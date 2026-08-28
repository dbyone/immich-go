// Package domain defines the entities persisted by the server. Field names
// and semantics mirror the tables of the upstream Immich v3.1 schema
// (user, session, api_key, asset, asset_exif, album, album_asset).
package domain

import "time"

const (
	AssetImage = "IMAGE"
	AssetVideo = "VIDEO"
	AssetAudio = "AUDIO"
	AssetOther = "OTHER"

	VisibilityTimeline = "timeline"
	VisibilityArchive  = "archive"
	VisibilityHidden   = "hidden"
	VisibilityLocked   = "locked"

	AlbumRoleEditor = "editor"
	AlbumRoleViewer = "viewer"
	AlbumRoleOwner  = "owner"
)

type User struct {
	ID                   string
	Email                string
	Password             string // bcrypt hash; empty when no password is set
	Name                 string
	IsAdmin              bool
	ShouldChangePassword bool
	AvatarColor          string
	ProfileImagePath     string
	StorageLabel         string
	IsOnboarded          bool
	Preferences          string // user preferences as raw JSON ("" = defaults)
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time

	// UpdateID is the incremental-sync watermark stamped on every write.
	UpdateID int64
}

type Session struct {
	ID          string
	TokenHash   []byte // SHA-256 of the opaque access token handed to the client
	UserID      string
	DeviceOS    string
	DeviceType  string
	AppVersion  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   *time.Time
}

type APIKey struct {
	ID          string
	Name        string
	KeyHash     []byte // SHA-256 of the secret shown once at creation
	UserID      string
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AssetExif struct {
	Make             string
	Model            string
	LensModel        string
	FileSize         int64
	ExifWidth        *int
	ExifHeight       *int
	DateTimeOriginal *time.Time
	Latitude         *float64
	Longitude        *float64
	City             string
	State            string
	Country          string
	Description      string
	Rating           *int
	FPS              *float64 // video frame rate
}

type Face struct {
	BoundingBox [4]int // x1, y1, x2, y2
	Embedding   string // base64 float32 vector from the ML service
	Score       float64
}

type Asset struct {
	ID               string
	OwnerID          string
	Type             string // IMAGE | VIDEO | AUDIO | OTHER
	OriginalPath     string
	ThumbnailPath    string
	PreviewPath      string
	OriginalFileName string
	OriginalMimeType string
	FileCreatedAt    time.Time
	FileModifiedAt   time.Time
	LocalDateTime    time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
	IsFavorite       bool
	Duration         *int64 // milliseconds
	Checksum         []byte // SHA-1, used for upload de-duplication
	ChecksumB64      string
	Width            *int
	Height           *int
	Visibility       string
	LibraryID        *string
	LivePhotoVideoID *string
	DuplicateID      *string
	Thumbhash        string
	Exif             *AssetExif
	Faces            []Face
	// SmartEmbedding holds the decoded 512-dim CLIP vector produced by the
	// machine-learning service (`smart_search.embedding` upstream).
	SmartEmbedding []float32
	// UpdateID is the incremental-sync watermark stamped on every write.
	UpdateID int64
}

// ExifDescription returns the searchable description text, if any.
func (a *Asset) ExifDescription() string {
	if a.Exif == nil {
		return ""
	}
	return a.Exif.Description
}

type AlbumUser struct {
	UserID string
	Role   string // editor | viewer
}

type Album struct {
	ID                    string
	OwnerID               string
	UpdateID              int64
	AlbumName             string
	Description           string
	AlbumThumbnailAssetID *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
	IsActivityEnabled     bool
	Order                 string // asc | desc
	AssetIDs              []string
	AssetIndex            map[string]bool
	Users                 []AlbumUser
}

func (a *Album) HasAsset(assetID string) bool {
	return a.AssetIndex != nil && a.AssetIndex[assetID]
}

// Memory mirrors the upstream memory entity (currently "on_this_day").
// Data holds the type-specific JSON payload ({"year": 2020}).
type Memory struct {
	ID        string
	OwnerID   string
	Type      string // on_this_day
	Data      string // JSON payload
	AssetIDs  []string
	MemoryAt  time.Time
	ShowAt    *time.Time
	HideAt    *time.Time
	SeenAt    *time.Time
	IsSaved   bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// SyncAck is one acknowledged sync entity ("<Type>:<updateId>" pairs).
type SyncAck struct {
	Type string
	Ack  string
}

// Stack groups related assets (e.g. burst shots); PrimaryAssetID leads.
type Stack struct {
	ID             string
	OwnerID        string
	PrimaryAssetID string
	AssetIDs       []string // ordered, primary first
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Partner is a library-sharing relation: OwnerID shares with UserID.
type Partner struct {
	ID         string
	OwnerID    string // sharedBy
	UserID     string // sharedWith
	InTimeline bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SyncDelete is a tombstone emitted when a synced entity goes away.
type SyncDelete struct {
	Type     string // AssetDeleteV1 / AlbumDeleteV1 / UserDeleteV1 ...
	EntityID string
	UpdateID int64
}
