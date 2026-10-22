package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReleaseCatalogResolvesLightweightAndAnnotatedTags(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tagObject := strings.Repeat("b", 40)
	for _, annotated := range []bool{false, true} {
		t.Run(fmt.Sprintf("annotated=%v", annotated), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("User-Agent") != "veil" || r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("missing GitHub request headers: %v", r.Header)
				}
				switch r.URL.Path {
				case "/repos/acme/veil/git/ref/tags/v1.2.3":
					if annotated {
						fmt.Fprintf(w, `{"object":{"type":"tag","sha":%q}}`, tagObject)
						return
					}
				case "/repos/acme/veil/git/tags/" + tagObject:
					if !annotated {
						t.Errorf("unexpected annotated lookup: %s", r.URL.Path)
					}
				default:
					t.Errorf("unexpected lookup: %s", r.URL.Path)
				}
				fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, strings.ToUpper(commit))
			}))
			defer server.Close()
			catalog := NewReleaseCatalog("acme", "veil")
			catalog.BaseURL = server.URL
			catalog.HTTPClient = server.Client()
			got, err := catalog.ResolveTagCommit(context.Background(), "v1.2.3")
			if err != nil || got != commit {
				t.Fatalf("ResolveTagCommit = %q, %v", got, err)
			}
			wantCalls := int32(1)
			if annotated {
				wantCalls = 2
			}
			if calls.Load() != wantCalls {
				t.Fatalf("lookups = %d, want %d", calls.Load(), wantCalls)
			}
		})
	}
}

func TestReleaseCatalogRejectsInvalidTagCommitResponses(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"short SHA", `{"object":{"type":"commit","sha":"abcdef1"}}`},
		{"nonhex SHA", `{"object":{"type":"commit","sha":"` + strings.Repeat("z", 40) + `"}}`},
		{"unsupported type", `{"object":{"type":"tree","sha":"` + strings.Repeat("a", 40) + `"}}`},
		{"malformed JSON", `{"object":`},
		{"missing object", `{}`},
		{"oversized JSON", `{"ignored":"` + strings.Repeat("x", 64<<10) + `"}`},
		{"unbounded annotation", `{"object":{"type":"tag","sha":"` + strings.Repeat("a", 40) + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			catalog := NewReleaseCatalog("acme", "veil")
			catalog.BaseURL = server.URL
			catalog.HTTPClient = server.Client()
			if got, err := catalog.ResolveTagCommit(context.Background(), "v1.2.3"); err == nil || got != "" {
				t.Fatalf("invalid tag resolution accepted: %q, %v", got, err)
			}
			if calls.Load() > 3 {
				t.Fatalf("tag chain exceeded bound: %d lookups", calls.Load())
			}
		})
	}
}

func TestReleaseCatalogProvesOnlyForwardImmutableSourceTransitions(t *testing.T) {
	base := strings.Repeat("a", 40)
	newer := strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, status, target, reportedBase, mergeBase string
		wantOK                                        bool
	}{
		{"forward", "ahead", newer, base, base, true},
		{"identical", "identical", base, base, base, true},
		{"newer source", "behind", newer, base, base, false},
		{"diverged", "diverged", newer, base, base, false},
		{"unknown status", "", newer, base, base, false},
		{"wrong base", "ahead", newer, newer, base, false},
		{"wrong merge base", "ahead", newer, base, newer, false},
		{"missing merge base", "ahead", newer, base, "", false},
		{"false identical", "identical", newer, base, base, false},
		{"false ahead", "ahead", base, base, base, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tagLookups atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/veil/git/ref/tags/v1.2.3":
					tagLookups.Add(1)
					fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, tc.target)
				case "/repos/acme/veil/compare/" + base + "..." + tc.target:
					json.NewEncoder(w).Encode(map[string]any{
						"status": tc.status, "base_commit": map[string]string{"sha": tc.reportedBase},
						"merge_base_commit": map[string]string{"sha": tc.mergeBase},
					})
				default:
					t.Errorf("comparison did not bind immutable SHAs: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			catalog := NewReleaseCatalog("acme", "veil")
			catalog.BaseURL = server.URL
			catalog.HTTPClient = server.Client()
			got, err := catalog.SourceUpdateCommit(context.Background(), "main-aaaaaaa ("+base+")", "v1.2.3")
			if tc.wantOK && (err != nil || got != tc.target) {
				t.Fatalf("forward proof = %q, %v", got, err)
			}
			if !tc.wantOK && (err == nil || got != "") {
				t.Fatalf("unsafe source transition accepted: %q, %v", got, err)
			}
			if tagLookups.Load() != 1 {
				t.Fatalf("mutable tag was resolved again: %d", tagLookups.Load())
			}
		})
	}
}

func TestReleaseCatalogSourceProofRejectsUnknownBuildsAndInvalidTagsBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()
	for _, tc := range []struct{ current, tag string }{
		{"dev", "v1.2.3"}, {"main-abcdef1", "v1.2.3"}, {"", "v1.2.3"},
		{"main-" + strings.Repeat("a", 40), "latest"}, {"main-" + strings.Repeat("a", 40), "v1.2.3+../bad"},
		{"main-" + strings.Repeat("a", 40), "v1.2.3 (abcdef1)"},
	} {
		if _, err := catalog.SourceUpdateCommit(context.Background(), tc.current, tc.tag); err == nil {
			t.Errorf("accepted unprovable source/target: %+v", tc)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid identity triggered %d HTTP calls", calls.Load())
	}
}

func TestReleaseCatalogSourceProofFailsClosedOnHTTPJSONAndContextErrors(t *testing.T) {
	base, target := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"forbidden", http.StatusForbidden, `{}`},
		{"not found", http.StatusNotFound, `{}`},
		{"invalid JSON", http.StatusOK, `not JSON`},
		{"missing proof", http.StatusOK, `{}`},
		{"oversized proof", http.StatusOK, `{"ignored":"` + strings.Repeat("x", 8<<20) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/git/ref/") {
					fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, target)
					return
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			catalog := NewReleaseCatalog("acme", "veil")
			catalog.BaseURL = server.URL
			catalog.HTTPClient = server.Client()
			if got, err := catalog.SourceUpdateCommit(context.Background(), "main-"+base, "v1.2.3"); err == nil || got != "" {
				t.Fatalf("incomplete proof accepted: %q, %v", got, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if got, err := catalog.SourceUpdateCommit(ctx, "main-"+base, "v1.2.3"); err == nil || got != "" {
		t.Fatalf("cancelled lookup accepted: %q, %v", got, err)
	}
}
