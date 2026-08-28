package api

import (
	"context"
	"time"

	"immich-go/internal/domain"
)

// DTO structs mirror the Immich OpenAPI schemas (camelCase JSON) so the
// official web/mobile clients can consume responses unchanged.

// ISOTime renders timestamps the way JS Date.toJSON does: UTC, millisecond
// precision, Z suffix.
type ISOTime time.Time

func (t ISOTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(t).UTC().Format("2006-01-02T15:04:05.000Z") + `"`), nil
}

func isoTimePtr(t *time.Time) *ISOTime {
	if t == nil {
		return nil
	}
	v := ISOTime(*t)
	return &v
}

type apiError struct {
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"statusCode"`
}

type UserResponse struct {
	ID               string  `json:"id"`
	Email            string  `json:"email"`
	Name             string  `json:"name"`
	AvatarColor      string  `json:"avatarColor"`
	ProfileImagePath string  `json:"profileImagePath"`
	ProfileChangedAt ISOTime `json:"profileChangedAt"`
}

type LoginResponse struct {
	AccessToken          string        `json:"accessToken"`
	UserID               string        `json:"userId"`
	UserEmail            string        `json:"userEmail"`
	Name                 string        `json:"name"`
	ProfileImagePath     string        `json:"profileImagePath"`
	IsAdmin              bool          `json:"isAdmin"`
	ShouldChangePassword bool          `json:"shouldChangePassword"`
	IsOnboarded          bool          `json:"isOnboarded"`
	User                 *UserResponse `json:"user,omitempty"`
}

type ValidateTokenResponse struct {
	AuthStatus bool `json:"authStatus"`
}

type LogoutResponse struct {
	RedirectURI string `json:"redirectUri"`
	Successful  bool   `json:"successful"`
}

type AuthStatusResponse struct {
	IsElevated bool `json:"isElevated"`
	Password   bool `json:"password"`
	PinCode    bool `json:"pinCode"`
}

type SessionResponse struct {
	ID                 string   `json:"id"`
	Token              string   `json:"token,omitempty"`
	CreatedAt          ISOTime  `json:"createdAt"`
	UpdatedAt          ISOTime  `json:"updatedAt"`
	ExpiresAt          *ISOTime `json:"expiresAt,omitempty"`
	Current            bool     `json:"current"`
	DeviceOS           string   `json:"deviceOS"`
	DeviceType         string   `json:"deviceType"`
	AppVersion         *string  `json:"appVersion"`
	IsPendingSyncReset bool     `json:"isPendingSyncReset"`
}

type APIKeyResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	CreatedAt   ISOTime  `json:"createdAt"`
	UpdatedAt   ISOTime  `json:"updatedAt"`
}

type APIKeyCreateResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Permissions []string        `json:"permissions"`
	Secret      string          `json:"secret"`
	CreatedAt   ISOTime         `json:"createdAt"`
	UpdatedAt   ISOTime         `json:"updatedAt"`
	APIKey      *APIKeyResponse `json:"apiKey"`
}

type ExifResponse struct {
	Make             string   `json:"make"`
	Model            string   `json:"model"`
	ExifImageWidth   *int     `json:"exifImageWidth"`
	ExifImageHeight  *int     `json:"exifImageHeight"`
	FileSizeInByte   *int64   `json:"fileSizeInByte"`
	DateTimeOriginal *ISOTime `json:"dateTimeOriginal,omitempty"`
	LensModel        string   `json:"lensModel"`
	FNumber          *float64 `json:"fNumber"`
	FocalLength      *float64 `json:"focalLength"`
	ISO              *int     `json:"iso"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	City             string   `json:"city"`
	State            string   `json:"state"`
	Country          string   `json:"country"`
	Description      string   `json:"description"`
	Rating           *int     `json:"rating"`
	FPS              *float64 `json:"fps"`
}

type AssetResponse struct {
	ID               string        `json:"id"`
	Checksum         string        `json:"checksum"`
	OwnerID          string        `json:"ownerId"`
	Owner            *UserResponse `json:"owner,omitempty"`
	Type             string        `json:"type"`
	OriginalPath     string        `json:"originalPath"`
	OriginalFileName string        `json:"originalFileName"`
	OriginalMimeType string        `json:"originalMimeType,omitempty"`
	Thumbhash        *string       `json:"thumbhash"`
	FileCreatedAt    ISOTime       `json:"fileCreatedAt"`
	FileModifiedAt   ISOTime       `json:"fileModifiedAt"`
	LocalDateTime    ISOTime       `json:"localDateTime"`
	CreatedAt        ISOTime       `json:"createdAt"`
	UpdatedAt        ISOTime       `json:"updatedAt"`
	IsFavorite       bool          `json:"isFavorite"`
	IsArchived       bool          `json:"isArchived"`
	IsTrashed        bool          `json:"isTrashed"`
	IsOffline        bool          `json:"isOffline"`
	IsEdited         bool          `json:"isEdited"`
	HasMetadata      bool          `json:"hasMetadata"`
	Duration         *int64        `json:"duration"`
	Width            *int          `json:"width"`
	Height           *int          `json:"height"`
	Visibility       string        `json:"visibility"`
	LivePhotoVideoID *string       `json:"livePhotoVideoId,omitempty"`
	LibraryID        *string       `json:"libraryId,omitempty"`
	DuplicateID      *string       `json:"duplicateId,omitempty"`
	ExifInfo         *ExifResponse `json:"exifInfo,omitempty"`
	Tags             []TagResponse `json:"tags"`
}

// TagResponse is the TagResponseDto wire shape.
type TagResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Value     string  `json:"value"`
	Color     string  `json:"color,omitempty"`
	ParentID  *string `json:"parentId,omitempty"`
	CreatedAt ISOTime `json:"createdAt"`
	UpdatedAt ISOTime `json:"updatedAt"`
}

type AssetMediaResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type AssetStatsResponse struct {
	Images int64 `json:"images"`
	Videos int64 `json:"videos"`
	Total  int64 `json:"total"`
}

type AlbumUserResponse struct {
	Role string       `json:"role"`
	User UserResponse `json:"user"`
}

type AlbumResponse struct {
	ID                         string              `json:"id"`
	AlbumName                  string              `json:"albumName"`
	AlbumThumbnailAssetID      *string             `json:"albumThumbnailAssetId"`
	AlbumUsers                 []AlbumUserResponse `json:"albumUsers"`
	AssetCount                 int                 `json:"assetCount"`
	CreatedAt                  ISOTime             `json:"createdAt"`
	UpdatedAt                  ISOTime             `json:"updatedAt"`
	StartDate                  *ISOTime            `json:"startDate,omitempty"`
	EndDate                    *ISOTime            `json:"endDate,omitempty"`
	LastModifiedAssetTimestamp *ISOTime            `json:"lastModifiedAssetTimestamp,omitempty"`
	Description                string              `json:"description"`
	HasSharedLink              bool                `json:"hasSharedLink"`
	IsActivityEnabled          bool                `json:"isActivityEnabled"`
	Shared                     bool                `json:"shared"`
	Order                      string              `json:"order"`
	Owner                      *UserResponse       `json:"owner"`
	// Assets is included by GET /albums/{id} for convenience; the columnar
	// timeline endpoints are the canonical asset listing for clients.
	Assets []AssetResponse `json:"assets,omitempty"`
}

type AlbumStatisticsResponse struct {
	NotShared int64 `json:"notShared"`
	Owned     int64 `json:"owned"`
	Shared    int64 `json:"shared"`
}

type BulkIDResponse struct {
	ID           string `json:"id"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type AlbumsAddAssetsResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type TimeBucketResponse struct {
	Count      int    `json:"count"`
	TimeBucket string `json:"timeBucket"`
}

// TimeBucketAssetResponse is the columnar payload of GET /timeline/bucket.
type TimeBucketAssetResponse struct {
	ID               []string   `json:"id"`
	CreatedAt        []ISOTime  `json:"createdAt"`
	FileCreatedAt    []ISOTime  `json:"fileCreatedAt"`
	Duration         []*int64   `json:"duration"`
	IsFavorite       []bool     `json:"isFavorite"`
	IsImage          []bool     `json:"isImage"`
	IsTrashed        []bool     `json:"isTrashed"`
	LivePhotoVideoID []*string  `json:"livePhotoVideoId"`
	LocalOffsetHours []float64  `json:"localOffsetHours"`
	OwnerID          []string   `json:"ownerId"`
	ProjectionType   []*string  `json:"projectionType"`
	Ratio            []float64  `json:"ratio"`
	Thumbhash        []*string  `json:"thumbhash"`
	Visibility       []string   `json:"visibility"`
	City             []*string  `json:"city,omitempty"`
	Country          []*string  `json:"country,omitempty"`
	Latitude         []*float64 `json:"latitude,omitempty"`
	Longitude        []*float64 `json:"longitude,omitempty"`
}

type SearchResponse struct {
	Albums []AlbumResponse `json:"albums"`
	Assets []AssetResponse `json:"assets"`
}

type ServerPingResponse struct {
	Res string `json:"res"`
}

type ServerVersionResponse struct {
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	Prerelease *int64 `json:"prerelease"`
}

type ServerVersionHistoryResponse struct {
	ID        string  `json:"id"`
	CreatedAt ISOTime `json:"createdAt"`
	Version   string  `json:"version"`
}

type ServerConfigResponse struct {
	ExternalDomain            string `json:"externalDomain"`
	IsInitialized             bool   `json:"isInitialized"`
	IsOnboarded               bool   `json:"isOnboarded"`
	LoginPageMessage          string `json:"loginPageMessage"`
	MaintenanceMode           bool   `json:"maintenanceMode"`
	MapDarkStyleURL           string `json:"mapDarkStyleUrl"`
	MapLightStyleURL          string `json:"mapLightStyleUrl"`
	MinFaces                  int    `json:"minFaces"`
	OAuthAccountManagementURL string `json:"oauthAccountManagementUrl"`
	OAuthButtonText           string `json:"oauthButtonText"`
	PublicUsers               bool   `json:"publicUsers"`
	TrashDays                 int    `json:"trashDays"`
	UserDeleteDelay           int    `json:"userDeleteDelay"`
}

type ServerFeaturesResponse struct {
	ConfigFile          bool `json:"configFile"`
	DuplicateDetection  bool `json:"duplicateDetection"`
	Email               bool `json:"email"`
	FacialRecognition   bool `json:"facialRecognition"`
	ImportFaces         bool `json:"importFaces"`
	Map                 bool `json:"map"`
	OAuth               bool `json:"oauth"`
	OAuthAutoLaunch     bool `json:"oauthAutoLaunch"`
	OCR                 bool `json:"ocr"`
	PasswordLogin       bool `json:"passwordLogin"`
	RealtimeTranscoding bool `json:"realtimeTranscoding"`
	ReverseGeocoding    bool `json:"reverseGeocoding"`
	Search              bool `json:"search"`
	Sidecar             bool `json:"sidecar"`
	SmartSearch         bool `json:"smartSearch"`
	Trash               bool `json:"trash"`
}

type ServerMediaTypesResponse struct {
	Image   []string `json:"image"`
	Sidecar []string `json:"sidecar"`
	Video   []string `json:"video"`
}

type ServerStatsResponse struct {
	Photos      int64               `json:"photos"`
	Videos      int64               `json:"videos"`
	Usage       int64               `json:"usage"`
	UsagePhotos int64               `json:"usagePhotos"`
	UsageVideos int64               `json:"usageVideos"`
	UsageByUser []ServerUsageByUser `json:"usageByUser"`
}

type ServerUsageByUser struct {
	UserID     string `json:"userId"`
	UserName   string `json:"userName"`
	Photos     int64  `json:"photos"`
	Videos     int64  `json:"videos"`
	UsageBytes int64  `json:"usage"`
	QuotaBytes int64  `json:"quotaSizeInBytes"`
}

type ServerStorageResponse struct {
	DiskAvailable    string  `json:"diskAvailable"`
	DiskAvailableRaw int64   `json:"diskAvailableRaw"`
	DiskSize         string  `json:"diskSize"`
	DiskSizeRaw      int64   `json:"diskSizeRaw"`
	DiskUsagePercent float64 `json:"diskUsagePercentage"`
	DiskUse          string  `json:"diskUse"`
	DiskUseRaw       int64   `json:"diskUseRaw"`
}

type QueueCountsDTO struct {
	Active    int64 `json:"active"`
	Completed int64 `json:"completed"`
	Delayed   int64 `json:"delayed"`
	Failed    int64 `json:"failed"`
	Paused    int64 `json:"paused"`
	Waiting   int64 `json:"waiting"`
}

type QueueStatusDTO struct {
	IsActive bool `json:"isActive"`
	IsPaused bool `json:"isPaused"`
}

type QueueLegacyDTO struct {
	JobCounts   QueueCountsDTO `json:"jobCounts"`
	QueueStatus QueueStatusDTO `json:"queueStatus"`
}

type ServerAboutResponse struct {
	Version                    string `json:"version"`
	VersionURL                 string `json:"versionUrl"`
	Repository                 string `json:"repository"`
	RepositoryURL              string `json:"repositoryUrl"`
	Licensed                   bool   `json:"licensed"`
	Build                      string `json:"build"`
	BuildURL                   string `json:"buildUrl"`
	BuildImage                 string `json:"buildImage"`
	BuildImageURL              string `json:"buildImageUrl"`
	SourceCommit               string `json:"sourceCommit"`
	SourceRef                  string `json:"sourceRef"`
	SourceURL                  string `json:"sourceUrl"`
	ThirdPartyBugFeatureURL    string `json:"thirdPartyBugFeatureUrl"`
	ThirdPartyDocumentationURL string `json:"thirdPartyDocumentationUrl"`
	ThirdPartySourceURL        string `json:"thirdPartySourceUrl"`
	ThirdPartySupportURL       string `json:"thirdPartySupportUrl"`
}

// assetResponse builds the wire representation of an asset.
func (s *Server) assetResponse(ctx context.Context, a *domain.Asset, withExif bool) AssetResponse {
	resp := AssetResponse{
		ID:               a.ID,
		Checksum:         a.ChecksumB64,
		OwnerID:          a.OwnerID,
		Type:             a.Type,
		OriginalPath:     a.OriginalPath,
		OriginalFileName: a.OriginalFileName,
		OriginalMimeType: a.OriginalMimeType,
		FileCreatedAt:    ISOTime(a.FileCreatedAt),
		FileModifiedAt:   ISOTime(a.FileModifiedAt),
		LocalDateTime:    ISOTime(a.LocalDateTime),
		CreatedAt:        ISOTime(a.CreatedAt),
		UpdatedAt:        ISOTime(a.UpdatedAt),
		IsFavorite:       a.IsFavorite,
		IsArchived:       a.Visibility == domain.VisibilityArchive,
		IsTrashed:        a.DeletedAt != nil,
		IsOffline:        false,
		IsEdited:         false,
		HasMetadata:      a.Exif != nil,
		Duration:         a.Duration,
		Width:            a.Width,
		Height:           a.Height,
		Visibility:       a.Visibility,
		Thumbhash:        nil,
		Tags:             []TagResponse{},
	}
	if a.Thumbhash != "" {
		resp.Thumbhash = &a.Thumbhash
	}
	if a.LivePhotoVideoID != nil {
		resp.LivePhotoVideoID = a.LivePhotoVideoID
	}
	if a.DuplicateID != nil {
		resp.DuplicateID = a.DuplicateID
	}
	if a.LibraryID != nil {
		resp.LibraryID = a.LibraryID
	}
	if withExif && a.Exif != nil {
		e := a.Exif
		resp.ExifInfo = &ExifResponse{
			Make:             e.Make,
			Model:            e.Model,
			ExifImageWidth:   e.ExifWidth,
			ExifImageHeight:  e.ExifHeight,
			DateTimeOriginal: isoTimePtr(e.DateTimeOriginal),
			LensModel:        e.LensModel,
			Latitude:         e.Latitude,
			Longitude:        e.Longitude,
			City:             e.City,
			State:            e.State,
			Country:          e.Country,
			Description:      e.Description,
			Rating:           e.Rating,
			FPS:              e.FPS,
		}
		if e.FileSize > 0 {
			size := e.FileSize
			resp.ExifInfo.FileSizeInByte = &size
		}
	}
	if u, err := s.app.Store.Users().Get(ctx, a.OwnerID); err == nil {
		resp.Owner = userResponsePtr(u)
	}
	// Tags ride on the asset row (upstream includes them in
	// AssetResponseDto); a missing store leaves the empty array.
	if s.app.Store != nil && s.app.Store.Tags() != nil {
		if tags, err := s.app.Store.Tags().ListForAsset(ctx, a.ID); err == nil {
			for _, t := range tags {
				resp.Tags = append(resp.Tags, tagResponse(t))
			}
		}
	}
	return resp
}

func tagResponse(t *domain.Tag) TagResponse {
	out := TagResponse{
		ID:        t.ID,
		Name:      t.Name,
		Value:     t.Value,
		ParentID:  t.ParentID,
		CreatedAt: ISOTime(t.CreatedAt),
		UpdatedAt: ISOTime(t.UpdatedAt),
	}
	if t.Color != nil {
		out.Color = *t.Color
	}
	return out
}

func userResponse(u *domain.User) UserResponse {
	return UserResponse{
		ID:               u.ID,
		Email:            u.Email,
		Name:             u.Name,
		AvatarColor:      u.AvatarColor,
		ProfileImagePath: u.ProfileImagePath,
		ProfileChangedAt: ISOTime(u.UpdatedAt),
	}
}

func userResponsePtr(u *domain.User) *UserResponse {
	r := userResponse(u)
	return &r
}
