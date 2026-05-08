package api

import (
	"net/http"

	"github.com/veil-panel/veil/internal/installer"
)

type RURecommendedPreviewRequest struct {
	Domain      string `json:"domain"`
	Email       string `json:"email"`
	Stack       string `json:"stack,omitempty"`
	PanelAccess string `json:"panelAccess,omitempty"`
}

type RURecommendedPreviewResponse struct {
	Domain             string `json:"domain"`
	Email              string `json:"email"`
	Stack              string `json:"stack"`
	InstallNaive       bool   `json:"installNaive"`
	InstallHysteria2   bool   `json:"installHysteria2"`
	InstallMieru       bool   `json:"installMieru"`
	PanelAccess        string `json:"panelAccess"`
	PanelURL           string `json:"panelUrl,omitempty"`
	Port               int    `json:"port"`
	NaiveClientURL     string `json:"naiveClientURL"`
	Hysteria2ClientURI string `json:"hysteria2ClientURI"`
	Caddyfile          string `json:"caddyfile"`
	Hysteria2YAML      string `json:"hysteria2YAML"`
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
	if req.Stack != "" && req.Stack != "panel" {
		writeError(w, "profile preview only supports Panel install; configure protocols as Panel Inbounds", http.StatusBadRequest)
		return
	}
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Domain:      req.Domain,
		Email:       req.Email,
		Stack:       installer.StackPanel,
		PanelAccess: req.PanelAccess,
		PanelPort:   2096,
		Availability: installer.PortAvailability{
			TCPBusy: map[int]bool{},
			UDPBusy: map[int]bool{},
		},
		Secret:     func(label string) string { return "preview-" + label },
		RandomPort: func() int { return 31874 },
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
		Domain:             profile.Domain,
		Email:              profile.Email,
		Stack:              string(profile.Stack),
		InstallNaive:       profile.InstallNaive,
		InstallHysteria2:   profile.InstallHysteria2,
		InstallMieru:       profile.InstallMieru,
		PanelAccess:        req.PanelAccess,
		PanelURL:           panelURL,
		Port:               profile.PortPlan.Port,
		NaiveClientURL:     redactProfileSecrets(profile, profile.NaiveClientURL),
		Hysteria2ClientURI: redactProfileSecrets(profile, profile.Hysteria2ClientURI),
		Caddyfile:          redactProfileSecrets(profile, profile.Caddyfile),
		Hysteria2YAML:      redactProfileSecrets(profile, profile.Hysteria2YAML),
	})
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	return NewCredentialDisclosure().RedactText(text, []string{profile.NaivePassword, profile.Hysteria2Password, profile.PanelAuthToken})
}
