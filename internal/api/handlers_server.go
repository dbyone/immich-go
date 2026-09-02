package api

import (
	"net/http"

	"immich-go/internal/maptile"
	"time"

	"github.com/go-chi/chi/v5"

	"immich-go/internal/auth"
	"immich-go/internal/config"
	"immich-go/internal/domain"
	"immich-go/internal/jobs"
)

func chiURLParam(r *http.Request, key string) string { return chi.URLParam(r, key) }

func versionString() string {
	return itoa(config.VersionMajor) + "." + itoa(config.VersionMinor) + "." + itoa(config.VersionPatch)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// --- server info ---

func (s *Server) serverPing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServerPingResponse{Res: "pong"})
}

func (s *Server) serverVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServerVersionResponse{
		Major: config.VersionMajor,
		Minor: config.VersionMinor,
		Patch: config.VersionPatch,
	})
}

func (s *Server) serverVersionHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []ServerVersionHistoryResponse{{
		ID:        "00000000-0000-4000-8000-000000000000",
		CreatedAt: ISOTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Version:   versionString(),
	}})
}

func (s *Server) serverAbout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServerAboutResponse{
		Version:       versionString(),
		VersionURL:    "https://github.com/immich-app/immich/releases",
		Repository:    "immich-go",
		RepositoryURL: "https://github.com/immich-app/immich",
		Licensed:      false,
		Build:         "go",
		BuildURL:      "",
	})
}

func (s *Server) serverMediaTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServerMediaTypesResponse{
		Image: []string{
			".3fr", ".ari", ".arw", ".avif", ".bmp", ".cap", ".cin", ".cr2", ".cr3", ".crw", ".dcr", ".dng", ".erf", ".fff",
			".gif", ".heic", ".heif", ".iiq", ".insp", ".jxl", ".jpeg", ".jpg", ".k25", ".kdc", ".mrw", ".nef", ".orf",
			".pef", ".png", ".psd", ".raf", ".raw", ".rw2", ".rwl", ".sr2", ".srf", ".srw", ".tiff", ".webp", ".x3f",
		},
		Sidecar: []string{".xml", ".xmp"},
		Video: []string{
			".3gp", ".avi", ".flv", ".m2ts", ".m4v", ".mkv", ".mov", ".mp4", ".mpg", ".mpeg", ".mts", ".webm", ".wmv",
		},
	})
}

func (s *Server) serverConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count, _ := s.app.Store.Users().Count(ctx)
	onboarded := false
	if count > 0 {
		if users, err := s.app.Store.Users().List(ctx); err == nil {
			for _, u := range users {
				if u.IsOnboarded {
					onboarded = true
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, ServerConfigResponse{
		IsInitialized:    count > 0,
		IsOnboarded:      onboarded,
		TrashDays:        30,
		UserDeleteDelay:  7,
		MinFaces:         s.minFaces(),
		PublicUsers:      true,
		MapLightStyleURL: maptile.StyleURL(s.app.Cfg.Map.Provider, "light", origin(r)),
		MapDarkStyleURL:  maptile.StyleURL(s.app.Cfg.Map.Provider, "dark", origin(r)),
		MapProvider:      maptile.Normalize(s.app.Cfg.Map.Provider),
		OAuthButtonText:  "Login with OAuth",
	})
}

func (s *Server) minFaces() int { return 3 }

// origin rebuilds this server's scheme://host from the request.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	return scheme + "://" + r.Host
}

// mapStyle serves the maplibre style document for the configured
// basemap provider (the AMap dialect points mapLight/DarkStyleUrl here).
func (s *Server) mapStyle(w http.ResponseWriter, r *http.Request) {
	theme := chiURLParam(r, "theme")
	if theme != "light" && theme != "dark" {
		writeError(w, http.StatusNotFound, "unknown map style")
		return
	}
	maptile.WriteStyle(w, s.app.Cfg.Map.Provider, theme)
}

func (s *Server) serverFeatures(w http.ResponseWriter, _ *http.Request) {
	ml := s.app.Cfg.MachineLearning
	writeJSON(w, http.StatusOK, ServerFeaturesResponse{
		ConfigFile:          false,
		DuplicateDetection:  ml.Enabled && ml.Clip.Enabled,
		Email:               false,
		FacialRecognition:   ml.Enabled && ml.FacialRecognition.Enabled,
		ImportFaces:         false,
		Map:                 true,
		OAuth:               false,
		OAuthAutoLaunch:     false,
		OCR:                 ml.Enabled && ml.OCR.Enabled,
		PasswordLogin:       true,
		RealtimeTranscoding: true,
		ReverseGeocoding:    true,
		Search:              true,
		Sidecar:             true,
		SmartSearch:         ml.Enabled && ml.Clip.Enabled,
		Trash:               true,
	})
}

func (s *Server) serverStorage(w http.ResponseWriter, _ *http.Request) {
	stats, err := diskStats(s.app.Storage.Root())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ServerStorageResponse{
		DiskAvailable:    humanBytes(stats.Available),
		DiskAvailableRaw: stats.Available,
		DiskSize:         humanBytes(stats.Total),
		DiskSizeRaw:      stats.Total,
		DiskUsagePercent: stats.UsagePercent(),
		DiskUse:          humanBytes(stats.Used),
		DiskUseRaw:       stats.Used,
	})
}

func (s *Server) serverStatistics(w http.ResponseWriter, r *http.Request) {
	a := auth.FromRequest(r)
	if a == nil {
		writeError(w, http.StatusUnauthorized, "Invalid token")
		return
	}
	if !a.User.IsAdmin {
		writeError(w, http.StatusForbidden, "Admin only")
		return
	}
	users, _ := s.app.Store.Users().List(r.Context())
	resp := ServerStatsResponse{UsageByUser: []ServerUsageByUser{}}
	for _, u := range users {
		assets, _ := s.app.Store.Assets().ListForOwner(r.Context(), u.ID)
		entry := ServerUsageByUser{UserID: u.ID, UserName: u.Name}
		for _, a2 := range assets {
			if a2.Type == domain.AssetVideo {
				entry.Videos++
				resp.Videos++
			} else {
				entry.Photos++
				resp.Photos++
			}
			if a2.Exif != nil {
				entry.UsageBytes += a2.Exif.FileSize
				resp.Usage += a2.Exif.FileSize
			}
		}
		resp.UsageByUser = append(resp.UsageByUser, entry)
	}
	resp.UsagePhotos = resp.Photos
	resp.UsageVideos = resp.Videos
	writeJSON(w, http.StatusOK, resp)
}

// --- jobs ---

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "adminQueue.read") {
		return
	}
	out := map[string]QueueLegacyDTO{}
	for name, q := range s.app.Jobs.Counts() {
		out[name] = QueueLegacyDTO{
			JobCounts:   QueueCountsDTO(q.Counts),
			QueueStatus: QueueStatusDTO(q.Status),
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type jobCreateRequest struct {
	Name string `json:"name"`
}

var manualJobs = map[string]bool{
	"person-cleanup": true, "tag-cleanup": true, "user-cleanup": true,
	"memory-cleanup": true, "memory-create": true, "backup-database": true,
	// immich-go extensions driving the DuckDB vector pipeline:
	"face-clustering":   true, // DBSCAN over face_search -> people
	"detect-duplicates": true, // CLIP near-duplicate groups
}

// manualJobTriggers maps manual job names to queued job names.
var manualJobTriggers = map[string]string{
	"face-clustering":   jobs.JobFacialRecognitionRun,
	"detect-duplicates": jobs.JobDuplicateDetectionRun,
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "adminQueue.run") {
		return
	}
	var req jobCreateRequest
	if err := decodeJSON(r, &req); err != nil || !manualJobs[req.Name] {
		writeError(w, http.StatusBadRequest, "invalid manual job name")
		return
	}
	if jobName, ok := manualJobTriggers[req.Name]; ok {
		if err := s.app.Jobs.Queue(jobName, map[string]string{"trigger": "manual"}); err != nil {
			s.writeInternal(w, err)
			return
		}
	}
	// Manual maintenance jobs are accepted; this port performs them lazily.
	w.WriteHeader(http.StatusAccepted)
}

type queueCommandRequest struct {
	Command string `json:"command"`
}

func (s *Server) commandJob(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "adminQueue.update") {
		return
	}
	name := chiURLParam(r, "name")
	var req queueCommandRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	switch req.Command {
	case "pause":
		if err := s.app.Jobs.SetPaused(name, true); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	case "resume":
		if err := s.app.Jobs.SetPaused(name, false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	case "empty":
		if err := s.app.Jobs.Empty(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "unknown command: "+req.Command)
		return
	}
	q := s.app.Jobs.Counts()[name]
	writeJSON(w, http.StatusOK, QueueLegacyDTO{
		JobCounts:   QueueCountsDTO(q.Counts),
		QueueStatus: QueueStatusDTO(q.Status),
	})
}
