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
	r, state := newSubscriptionTestRouter(t)
	plaintext, clientID := seedClientWithToken(t, r)

	// Record real traffic against the durable normalized binding.
	var bindingID string
	if err := state.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=? LIMIT 1`, clientID).Scan(&bindingID); err != nil {
		t.Fatalf("lookup binding: %v", err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID:     bindingID,
		ClientID:      clientID,
		UploadBytes:   1234,
		DownloadBytes: 5678,
		AtUnix:        1000,
	}); err != nil {
		t.Fatalf("record traffic: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	h := w.Header().Get("Subscription-Userinfo")
	// Lock the exact recorded totals — a non-zero-but-wrong pair must fail
	// just as hard as zeros (#831).
	if up := headerIntField(h, "upload"); up != 1234 {
		t.Errorf("Subscription-Userinfo upload=%d want 1234: %q", up, h)
	}
	if down := headerIntField(h, "download"); down != 5678 {
		t.Errorf("Subscription-Userinfo download=%d want 5678: %q", down, h)
	}
	if total := headerIntField(h, "total"); total != 0 {
		// No quota was configured for this client; the field must be absent
		// or zero rather than echoing a fabricated cap.
		t.Errorf("Subscription-Userinfo total=%d want unset/0: %q", total, h)
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
