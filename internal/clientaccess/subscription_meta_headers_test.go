package clientaccess

import (
	"net/http"
	"testing"
)

func header(m map[string][]string) http.Header { return http.Header(m) }

func TestSubscriptionMetaHeadersUserinfo(t *testing.T) {
	total := int64(1000000)
	expire := int64(1700000000)
	h := SubscriptionMetaHeaders{Upload: 100, Download: 200, Total: &total, Expire: &expire}
	hdr := make(map[string][]string)
	got := header(hdr)
	h.Apply(got)
	ui := got.Get("Subscription-Userinfo")
	if ui != "upload=100; download=200; total=1000000; expire=1700000000" {
		t.Fatalf("userinfo = %q", ui)
	}
}

func TestSubscriptionMetaHeadersOmitsNilFields(t *testing.T) {
	h := SubscriptionMetaHeaders{Upload: 5, Download: 7}
	got := header(make(map[string][]string))
	h.Apply(got)
	ui := got.Get("Subscription-Userinfo")
	if ui != "upload=5; download=7" {
		t.Fatalf("userinfo without nil fields = %q", ui)
	}
}

func TestSubscriptionMetaHeadersUpdateIntervalAndTitle(t *testing.T) {
	h := SubscriptionMetaHeaders{ProfileUpdateIntervalHours: 24, ProfileTitle: "My VPN", SupportURL: "https://help.example"}
	got := header(make(map[string][]string))
	h.Apply(got)
	if got.Get("Profile-Update-Interval") != "24" {
		t.Fatalf("interval = %q", got.Get("Profile-Update-Interval"))
	}
	if got.Get("Profile-Title") != "My VPN" {
		t.Fatalf("title = %q", got.Get("Profile-Title"))
	}
	if got.Get("Support-URL") != "https://help.example" {
		t.Fatalf("support-url = %q", got.Get("Support-URL"))
	}
	if cd := got.Get("Content-Disposition"); cd != `attachment; filename="My VPN.txt"` {
		t.Fatalf("content-disposition = %q", cd)
	}
}

func TestSubscriptionMetaHeadersSanitizesTitle(t *testing.T) {
	h := SubscriptionMetaHeaders{ProfileTitle: `bad/name";evil`}
	got := header(make(map[string][]string))
	h.Apply(got)
	if cd := got.Get("Content-Disposition"); cd != `attachment; filename="bad-name--evil.txt"` {
		t.Fatalf("sanitized content-disposition = %q", cd)
	}
}
