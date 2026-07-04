package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDownloadPanelUpdateAsset(t *testing.T) {
	cases := []struct {
		name        string
		statusCode  int
		body        []byte
		roundErr    error
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       []byte("archive-body"),
		},
		{
			name:        "server error",
			statusCode:  http.StatusInternalServerError,
			body:        []byte("boom"),
			wantErr:     true,
			errContains: "Internal Server Error",
		},
		{
			name:        "request error",
			roundErr:    errors.New("network unreachable"),
			wantErr:     true,
			errContains: "network unreachable",
		},
		{
			name:        "oversized asset",
			statusCode:  http.StatusOK,
			body:        bytes.Repeat([]byte("x"), int(maxPanelUpdateDownloadBytes)+1),
			wantErr:     true,
			errContains: "size limit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if tc.roundErr != nil {
						return nil, tc.roundErr
					}
					return &http.Response{
						StatusCode: tc.statusCode,
						Status:     http.StatusText(tc.statusCode),
						Body:       io.NopCloser(bytes.NewReader(tc.body)),
						Header:     make(http.Header),
					}, nil
				}),
			}
			body, err := downloadPanelUpdateAsset(context.Background(), client, "https://example.invalid/asset")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.errContains != "" && !bytes.Contains([]byte(err.Error()), []byte(tc.errContains)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(body, tc.body) {
				t.Fatalf("body mismatch")
			}
		})
	}
}
