package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// defaultPreferences mirrors the upstream UserPreferencesResponseDto
// defaults; stored user JSON overlays it key by key.
const defaultPreferences = `{
	"albums": {"defaultAssetOrder": "asc"},
	"cast": {"gCastEnabled": false},
	"download": {"archiveSize": 4294967296, "includeEmbeddedVideos": false},
	"emailNotifications": {"albumInvite": true, "albumUpdate": true, "enabled": true},
	"folders": {"enabled": false, "sidebarWeb": false},
	"memories": {"duration": 5259600, "enabled": true, "sidebarWeb": true},
	"people": {"enabled": true, "minimumFaces": 3, "sidebarWeb": true},
	"purchase": {"hideBuyButtonUntil": "", "showSupportBadge": true},
	"ratings": {"enabled": false},
	"recentlyAdded": {"sidebarWeb": false},
	"sharedLinks": {"enabled": true},
	"tags": {"enabled": false, "sidebarWeb": false}
}`

// mergeOverDefaults deep-merges the stored JSON over the defaults so new
// preference keys keep working for clients with older payloads.
func mergeOverDefaults(stored string) map[string]any {
	var base, override map[string]any
	if err := json.Unmarshal([]byte(defaultPreferences), &base); err != nil {
		return map[string]any{}
	}
	if stored != "" {
		if err := json.Unmarshal([]byte(stored), &override); err == nil {
			deepMerge(base, override)
		}
	}
	return base
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if dstSub, ok := dst[k].(map[string]any); ok {
				deepMerge(dstSub, sub)
				continue
			}
		}
		dst[k] = v
	}
}

func (s *Server) getMyPreferences(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	writeJSON(w, http.StatusOK, mergeOverDefaults(a.User.Preferences))
}

func (s *Server) updateMyPreferences(w http.ResponseWriter, r *http.Request) {
	a := caller(w, r)
	if a == nil {
		return
	}
	var body json.RawMessage
	if err := decodeJSON(r, &body); err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "invalid preferences payload")
		return
	}
	var check map[string]any
	if err := json.Unmarshal(body, &check); err != nil || check == nil {
		writeError(w, http.StatusBadRequest, "preferences must be a JSON object")
		return
	}
	user := a.User
	user.Preferences = string(body)
	if err := s.app.Store.Users().Update(r.Context(), user); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mergeOverDefaults(user.Preferences))
}

// ---- onboarding / system metadata ----

const onboardingKey = "admin-onboarding"

func (s *Server) getAdminOnboarding(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "adminConfig.read") {
		return
	}
	_, set, err := s.app.Store.Metadata().Get(r.Context(), onboardingKey)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"isOnboarded": set})
}

func (s *Server) updateAdminOnboarding(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "adminConfig.update") {
		return
	}
	var req struct {
		IsOnboarded bool `json:"isOnboarded"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	v := "false"
	if req.IsOnboarded {
		v = "true"
	}
	if err := s.app.Store.Metadata().Set(r.Context(), onboardingKey, v); err != nil {
		s.writeInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- user config (/config) + public config ----

func (s *Server) buildUserConfig() map[string]any {
	ml := s.app.Cfg.MachineLearning
	return map[string]any{
		"ffmpeg": map[string]any{
			"realtime": map[string]any{
				"enabled":     false,
				"resolutions": []int{720, 1080, 1440, 2160},
				"videoCodecs": []string{"h264", "hevc", "vp9", "av1"},
			},
		},
		"image": map[string]any{
			"fullsize":  map[string]any{"enabled": false},
			"preview":   map[string]any{"size": 1440},
			"thumbnail": map[string]any{"size": 250},
		},
		"machineLearning": map[string]any{
			"enabled":            ml.Enabled,
			"clip":               map[string]any{"enabled": ml.Clip.Enabled},
			"duplicateDetection": map[string]any{"enabled": ml.DuplicateDetection.Enabled},
			"facialRecognition": map[string]any{
				"enabled":     ml.FacialRecognition.Enabled,
				"minFaces":    ml.FacialRecognition.MinFaces,
				"minScore":    ml.FacialRecognition.MinScore,
				"maxDistance": ml.FacialRecognition.MaxDistance,
			},
			"ocr": map[string]any{"enabled": ml.OCR.Enabled},
		},
		"map": map[string]any{"enabled": true, "lightStyle": "", "darkStyle": ""},
		"oauth": map[string]any{
			"enabled": false, "autoLaunch": false, "buttonText": "",
		},
		"passwordLogin":   map[string]any{"enabled": true},
		"server":          map[string]any{"loginPageMessage": ""},
		"theme":           map[string]any{"customCss": ""},
		"trash":           map[string]any{"enabled": true, "days": 30},
		"newVersionCheck": map[string]any{"enabled": false},
		"job":             map[string]any{}, // per-queue concurrency tuning not exposed
	}
}

func (s *Server) getUserConfig(w http.ResponseWriter, r *http.Request) {
	if caller(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, s.buildUserConfig())
}

func (s *Server) getUserConfigDefaults(w http.ResponseWriter, r *http.Request) {
	if caller(w, r) == nil {
		return
	}
	writeJSON(w, http.StatusOK, s.buildUserConfig())
}

func publicConfig() map[string]any {
	return map[string]any{
		"oauth":         map[string]any{"autoLaunch": false, "buttonText": "", "enabled": false},
		"passwordLogin": map[string]any{"enabled": true},
		"server":        map[string]any{"loginPageMessage": ""},
		"theme":         map[string]any{"customCss": ""},
	}
}

func (s *Server) getPublicConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicConfig())
}

func (s *Server) getPublicConfigDefaults(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicConfig())
}

// ---- misc server endpoints ----

func (s *Server) serverApkLinks(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "server.read") {
		return
	}
	const base = "https://github.com/immich-app/immich/releases/latest/download/"
	writeJSON(w, http.StatusOK, map[string]string{
		"arm64v8a":   base + "app-arm64-v8a.apk",
		"armeabiv7a": base + "app-armeabi-v7a.apk",
		"universal":  base + "app-universal.apk",
		"x86_64":     base + "app-x86_64.apk",
	})
}

func (s *Server) serverVersionCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "server.read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"checkedAt":      ISOTime(time.Now().UTC()),
		"releaseVersion": versionString(),
	})
}
