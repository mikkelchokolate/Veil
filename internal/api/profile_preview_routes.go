package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/installer"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
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
	CaddyJSON   string `json:"caddyJSON,omitempty"`
}

type ProfilePreviewRoutes struct{}

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
		CaddyJSON:   redactProfileSecrets(profile, profile.CaddyJSON),
	})
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	return veilsettings.NewCredentialDisclosure().RedactText(text, []string{profile.PanelAuthToken})
}
