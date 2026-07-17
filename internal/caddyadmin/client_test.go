package caddyadmin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadConfigPostsJSON(t *testing.T) {
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/load" {
			t.Errorf("path = %s", r.URL.Path)
		}
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		received = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.LoadConfig([]byte(`{"apps":{}}`)); err != nil {
		t.Fatal(err)
	}
	if received == "" {
		t.Error("server received empty body")
	}
}
