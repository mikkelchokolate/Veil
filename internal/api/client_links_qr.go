package api

import (
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const maxClientLinkQRBytes = 4096

func (s *managementState) handleClientLinkQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req struct {
		URI string `json:"uri"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	uri := strings.TrimSpace(req.URI)
	if uri == "" {
		writeError(w, "uri is required", http.StatusBadRequest)
		return
	}
	if len(uri) > maxClientLinkQRBytes {
		writeError(w, "uri is too large for QR export", http.StatusRequestEntityTooLarge)
		return
	}
	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		writeError(w, "failed to render QR code", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `inline; filename="veil-client-link-qr.png"`)
	_, _ = w.Write(png)
}
