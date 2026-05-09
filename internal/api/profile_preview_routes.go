package api

import (
	"net/http"

	"github.com/veil-panel/veil/internal/installer"
)

type RURecommendedPreviewRequest struct {
	Domain      string `json:"domain"`
	Email       string `json:"email"`
	PanelAccess string `json:"panelAccess,omitempty"`
}

type RURecommendedPreviewResponse struct {
	Domain      string `json:"domain"`
	Email       string `json:"email"`
	PanelAccess string `json:"panelAccess"`
	PanelURL    string `json:"panelUrl,omitempty"`
	Caddyfile   string `json:"caddyfile,omitempty"`
}

type ProfilePreviewRoutes struct{}

func (ProfilePreviewRoutes) Paths() []string {
	return []string{"/api/profiles/ru-recommended/preview"}
}

func (routes ProfilePreviewRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/profiles/ru-recommended/preview", routes.handleRURecommendedPreview)
}

func (ProfilePreviewRoutes) handleRURecommendedPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req RURecommendedPreviewRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Domain:      req.Domain,
		Email:       req.Email,
		PanelAccess: req.PanelAccess,
		PanelPort:   2096,
		Secret:      func(label string) string { return "preview-" + label },
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	panelURL := ""
	if req.PanelAccess == "caddy" && profile.WebBasePath != "" {
		panelURL = "https://" + profile.Domain + profile.WebBasePath
	}
	writeJSON(w, RURecommendedPreviewResponse{
		Domain:      profile.Domain,
		Email:       profile.Email,
		PanelAccess: req.PanelAccess,
		PanelURL:    panelURL,
		Caddyfile:   redactProfileSecrets(profile, profile.Caddyfile),
	})
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	return NewCredentialDisclosure().RedactText(text, []string{profile.PanelAuthToken})
}
