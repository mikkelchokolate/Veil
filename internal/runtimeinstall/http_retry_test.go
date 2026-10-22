package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

type runtimeRetryTransport struct {
	calls     int
	roundTrip func(*http.Request) (*http.Response, error)
}

func (r *runtimeRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.calls++
	return r.roundTrip(request)
}

func TestRuntimeHTTPRetriesTemporaryReleaseAndAssetFailures(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "asset", true: "release"}[release], func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					http.Error(w, "temporary", http.StatusServiceUnavailable)
					return
				}
				if release {
					_, _ = io.WriteString(w, `{"tag_name":"v1","assets":[{"name":"runtime","browser_download_url":"https://example.test/runtime"}]}`)
				} else {
					_, _ = io.WriteString(w, "verified later")
				}
			}))
			defer server.Close()
			var err error
			if release {
				_, err = fetchReleaseAt(t.Context(), server.Client(), server.URL, "", "v1")
			} else {
				_, err = downloadURL(t.Context(), server.Client(), server.URL)
			}
			if err != nil || calls != 2 {
				t.Fatalf("temporary failure: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestRuntimeHTTPRetryIsBoundedAndDoesNotRetryPermanentFailures(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
		transport := &runtimeRetryTransport{roundTrip: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader("failure")), Header: make(http.Header)}, nil
		}}
		_, err := downloadURL(t.Context(), &http.Client{Transport: transport}, "https://example.test/runtime")
		want := 1
		if status == http.StatusServiceUnavailable {
			want = 3
		}
		if err == nil || transport.calls != want {
			t.Fatalf("status=%d calls=%d want=%d error=%v", status, transport.calls, want, err)
		}
	}
}

func TestRuntimeHTTPBackoffHonorsCancellationAndClosesResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	transport := &runtimeRetryTransport{roundTrip: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: &runtimeRetryBody{close: func() { closed = true; cancel() }}}, nil
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/runtime", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtimeHTTPDo(&http.Client{Transport: transport}, request)
	if !errors.Is(err, context.Canceled) || transport.calls != 1 || !closed {
		t.Fatalf("cancelled retry: calls=%d closed=%v error=%v", transport.calls, closed, err)
	}
}

func TestRuntimeDownloadRetriesMidBodyFailureAndClosesBeforeRetry(t *testing.T) {
	for name, readErr := range map[string]error{"truncated": io.ErrUnexpectedEOF, "reset": syscall.ECONNRESET} {
		t.Run(name, func(t *testing.T) {
			closed := false
			transport := &runtimeRetryTransport{}
			transport.roundTrip = func(*http.Request) (*http.Response, error) {
				if transport.calls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Body: &runtimeDownloadBody{
						Reader: io.MultiReader(strings.NewReader("partial"), &errorReader{err: readErr}),
						close:  func() { closed = true },
					}}, nil
				}
				if !closed {
					t.Fatal("retry began before the truncated response was closed")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("complete"))}, nil
			}
			body, err := downloadURL(t.Context(), &http.Client{Transport: transport}, "https://example.test/runtime")
			if err != nil || string(body) != "complete" || transport.calls != 2 || !closed {
				t.Fatalf("mid-body retry: calls=%d closed=%v body=%q error=%v", transport.calls, closed, body, err)
			}
		})
	}
}

func TestRuntimeDownloadSharesOneAttemptBudgetForStatusAndBodyFailures(t *testing.T) {
	closed := 0
	transport := &runtimeRetryTransport{}
	transport.roundTrip = func(*http.Request) (*http.Response, error) {
		status := http.StatusOK
		if transport.calls == 1 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Body: &runtimeDownloadBody{
			Reader: &errorReader{err: io.ErrUnexpectedEOF}, close: func() { closed++ },
		}}, nil
	}
	_, err := downloadURL(t.Context(), &http.Client{Transport: transport}, "https://example.test/runtime")
	if !errors.Is(err, io.ErrUnexpectedEOF) || transport.calls != 3 || closed != 3 {
		t.Fatalf("shared retry budget: calls=%d closed=%d error=%v", transport.calls, closed, err)
	}
}

func TestRuntimeDownloadBodyRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	transport := &runtimeRetryTransport{roundTrip: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &runtimeDownloadBody{
			Reader: &errorReader{err: io.ErrUnexpectedEOF}, close: func() { closed = true; cancel() },
		}}, nil
	}}
	_, err := downloadURL(ctx, &http.Client{Transport: transport}, "https://example.test/runtime")
	if !errors.Is(err, context.Canceled) || transport.calls != 1 || !closed {
		t.Fatalf("cancelled body retry: calls=%d closed=%v error=%v", transport.calls, closed, err)
	}
}

type runtimeDownloadBody struct {
	io.Reader
	close func()
}

func (b *runtimeDownloadBody) Close() error { b.close(); return nil }

type runtimeRetryBody struct{ close func() }

func (*runtimeRetryBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *runtimeRetryBody) Close() error           { b.close(); return nil }
