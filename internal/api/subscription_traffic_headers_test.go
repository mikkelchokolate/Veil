package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// TestSubscriptionUserinfoUsesRealTraffic asserts (A11) that the
// Subscription-Userinfo header carries the client's real recorded traffic
// totals, not hardcoded zeros.
func TestSubscriptionUserinfoUsesRealTraffic(t *testing.T) {
	r, state := newTrafficRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)

	// Record real traffic for this client.
	_ = state.trafficStore.RecordSample(client.Sample{
		BindingID:     "b1",
		ClientID:      clientID,
		UploadBytes:   1234,
		DownloadBytes: 5678,
		AtUnix:        1000,
	})

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	h := w.Header().Get("Subscription-Userinfo")
	up := headerIntField(h, "upload")
	down := headerIntField(h, "download")
	if up == 0 && down == 0 {
		t.Errorf("Subscription-Userinfo carries zeros despite recorded traffic (A11): %q", h)
	}
}

func headerIntField(h, key string) int64 {
	for _, part := range strings.Split(h, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+"=") {
			v, _ := strconv.ParseInt(strings.TrimPrefix(part, key+"="), 10, 64)
			return v
		}
	}
	return 0
}
