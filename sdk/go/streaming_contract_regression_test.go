package veilclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBackupDownloadStreamingMethodReturnsTypedUnbufferedResponse(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/backups/archive.enc/download" {
			t.Errorf("request path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="archive.enc"`)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("prefix-"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-release
		_, _ = w.Write([]byte("suffix"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/panel/")
	if err != nil {
		t.Fatal(err)
	}
	method := reflect.ValueOf(client).MethodByName("GetApiBackupsNameDownloadStream")
	if !method.IsValid() {
		t.Fatal("generated client lacks GetApiBackupsNameDownloadStream")
	}
	type callResult struct {
		values []reflect.Value
	}
	returned := make(chan callResult, 1)
	go func() {
		inputs := []reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(BackupName("archive.enc"))}
		if method.Type().IsVariadic() {
			inputs = append(inputs, reflect.MakeSlice(method.Type().In(method.Type().NumIn()-1), 0, 0))
			returned <- callResult{values: method.CallSlice(inputs)}
			return
		}
		returned <- callResult{values: method.Call(inputs)}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream request never reached server")
	}
	var values []reflect.Value
	select {
	case result := <-returned:
		values = result.values
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("stream method buffered until EOF instead of returning headers/body")
	}
	if len(values) != 2 || !values[1].IsNil() {
		close(release)
		t.Fatalf("stream call result=%v", values)
	}
	response := values[0]
	if response.Kind() == reflect.Pointer {
		response = response.Elem()
	}
	status := response.FieldByName("StatusCode")
	headers := response.FieldByName("Header")
	bodyField := response.FieldByName("Body")
	if !status.IsValid() || int(status.Int()) != http.StatusPartialContent {
		close(release)
		t.Fatalf("typed stream status missing: %v", status)
	}
	if !headers.IsValid() || headers.Interface().(http.Header).Get("Content-Disposition") == "" {
		close(release)
		t.Fatal("typed stream headers missing")
	}
	body, ok := bodyField.Interface().(io.ReadCloser)
	if !ok {
		close(release)
		t.Fatal("typed stream body is not io.ReadCloser")
	}
	prefix := make([]byte, len("prefix-"))
	if _, err := io.ReadFull(body, prefix); err != nil || string(prefix) != "prefix-" {
		close(release)
		t.Fatalf("read streamed prefix=%q err=%v", prefix, err)
	}
	close(release)
	remainder, err := io.ReadAll(body)
	if err != nil || string(remainder) != "suffix" {
		t.Fatalf("read streamed suffix=%q err=%v", remainder, err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close stream body: %v", err)
	}
}

func TestSDKURLResolutionRejectsAbsoluteOperationInput(t *testing.T) {
	body, err := os.ReadFile("veilclient_streaming.gen.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, required := range []string{"resolveRelativeURL", "IsAbs()", "absolute API path"} {
		if !strings.Contains(source, required) {
			t.Errorf("generated URL construction lacks %q", required)
		}
	}
}
