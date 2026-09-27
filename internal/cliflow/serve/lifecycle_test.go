package serve

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakeReloader struct {
	reloadCalled       int32
	reloadErr          error
	closeCalled        int32
	closeErr           error
	closeStreamsCalled int32
	closeStreamsHook   func()
}

func (f *fakeReloader) Reload() error {
	atomic.AddInt32(&f.reloadCalled, 1)
	return f.reloadErr
}

func (f *fakeReloader) Close() error {
	atomic.AddInt32(&f.closeCalled, 1)
	return f.closeErr
}

func (f *fakeReloader) CloseStreams() {
	atomic.AddInt32(&f.closeStreamsCalled, 1)
	if f.closeStreamsHook != nil {
		f.closeStreamsHook()
	}
}

func TestRunServeLifecycleClosesStateWorkersOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reloader := &fakeReloader{}
	err := RunLifecycle(LifecycleOptions{
		Context:       ctx,
		Out:           &bytes.Buffer{},
		Err:           &bytes.Buffer{},
		Server:        &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()},
		StateReloader: reloader,
	})
	if err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}
	if got := atomic.LoadInt32(&reloader.closeCalled); got != 1 {
		t.Fatalf("state lifecycle close calls=%d, want 1", got)
	}
}

func TestRunServeLifecycleShutsDownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer

	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	cancel()
	if err := RunLifecycle(LifecycleOptions{Context: ctx, Out: &out, Err: &out, Server: server}); err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}
	if !strings.Contains(out.String(), "Shutting down") || !strings.Contains(out.String(), "Server stopped") {
		t.Fatalf("shutdown output missing:\n%s", out.String())
	}
}

func TestRunLifecycleReturnsServerError(t *testing.T) {
	wantErr := errors.New("boom")
	lifecycleListenAndServe = func(srv *http.Server) error { return wantErr }
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	err := RunLifecycle(LifecycleOptions{
		Context: context.Background(),
		Out:     &bytes.Buffer{},
		Err:     &bytes.Buffer{},
		Server:  &http.Server{Addr: "127.0.0.1:0"},
	})
	if err == nil || !strings.Contains(err.Error(), "server error") {
		t.Fatalf("expected server error, got %v", err)
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("expected wrapped error %q, got %v", wantErr, err)
	}
}

func TestRunLifecycleIgnoresErrServerClosed(t *testing.T) {
	lifecycleListenAndServe = func(srv *http.Server) error { return http.ErrServerClosed }
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	err := RunLifecycle(LifecycleOptions{
		Context: context.Background(),
		Out:     &bytes.Buffer{},
		Err:     &bytes.Buffer{},
		Server:  &http.Server{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatalf("expected nil for ErrServerClosed, got %v", err)
	}
}

func TestRunLifecycleHandlesSIGHUPReload(t *testing.T) {
	started := make(chan struct{})
	var closeStarted sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	lifecycleListenAndServe = func(srv *http.Server) error {
		closeStarted.Do(func() { close(started) })
		<-ctx.Done()
		return http.ErrServerClosed
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	var out bytes.Buffer
	var errOut bytes.Buffer
	reloader := &fakeReloader{}

	go func() {
		<-started
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Errorf("kill: %v", err)
		}
		// Give the signal handler a moment to run before cancelling.
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	if err := RunLifecycle(LifecycleOptions{
		Context:       ctx,
		Out:           &out,
		Err:           &errOut,
		Server:        &http.Server{Addr: "127.0.0.1:0"},
		StateReloader: reloader,
	}); err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}

	if atomic.LoadInt32(&reloader.reloadCalled) != 1 {
		t.Fatalf("expected reload to be called once, got %d", reloader.reloadCalled)
	}
	if !strings.Contains(out.String(), "State reloaded") {
		t.Fatalf("missing reload output:\n%s", out.String())
	}
}

func TestRunLifecycleHandlesSIGHUPReloadError(t *testing.T) {
	started := make(chan struct{})
	var closeStarted sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	lifecycleListenAndServe = func(srv *http.Server) error {
		closeStarted.Do(func() { close(started) })
		<-ctx.Done()
		return http.ErrServerClosed
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	var out bytes.Buffer
	var errOut bytes.Buffer
	reloader := &fakeReloader{reloadErr: errors.New("reload failed")}

	go func() {
		<-started
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Errorf("kill: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	if err := RunLifecycle(LifecycleOptions{
		Context:       ctx,
		Out:           &out,
		Err:           &errOut,
		Server:        &http.Server{Addr: "127.0.0.1:0"},
		StateReloader: reloader,
	}); err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}

	if !strings.Contains(errOut.String(), "reload error") {
		t.Fatalf("missing reload error output:\n%s", errOut.String())
	}
}

func TestRunLifecycleDefaults(t *testing.T) {
	// Nil Out/Err default to io.Discard: the shutdown path writes
	// "Shutting down"/"Server stopped" to both, so unset writers would panic
	// if the defaults were not applied. Drive the full cancel → Shutdown →
	// serve-exit path with them nil, and leave DrainTimeout unset so the
	// default 5s budget bounds the graceful shutdown.
	serveStarted := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	lifecycleListenAndServe = func(srv *http.Server) error {
		close(serveStarted)
		<-ctx.Done()
		return http.ErrServerClosed
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	done := make(chan error, 1)
	go func() {
		done <- RunLifecycle(LifecycleOptions{
			Context: ctx,
			Server:  &http.Server{Addr: "127.0.0.1:0"},
		})
	}()
	<-serveStarted
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunLifecycle with default Out/Err/DrainTimeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle did not finish shutdown")
	}

	// A nil Context defaults to context.Background; the only way out is the
	// serve result itself.
	lifecycleListenAndServe = func(srv *http.Server) error { return http.ErrServerClosed }
	if err := RunLifecycle(LifecycleOptions{Server: &http.Server{Addr: "127.0.0.1:0"}}); err != nil {
		t.Fatalf("RunLifecycle all-defaults: %v", err)
	}
}

// A connection that can never go idle (e.g. an SSE stream whose hub was
// never closed) no longer turns `systemctl stop` into a failure: after the
// drain timeout the lifecycle warns and force-closes, returning nil
// (issue #1110).
func TestRunLifecycleShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	entered := make(chan struct{})
	server := &http.Server{
		Addr: listener.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
		}),
	}

	lifecycleListenAndServe = func(srv *http.Server) error {
		return srv.Serve(listener)
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunLifecycle(LifecycleOptions{
			Context:      ctx,
			Out:          &out,
			Err:          &errOut,
			Server:       server,
			DrainTimeout: 1 * time.Nanosecond,
		})
	}()

	// Wait for the server to accept connections.
	time.Sleep(50 * time.Millisecond)

	// Open a connection and hold it open through shutdown.
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking handler did not start")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("drain timeout must be a warning, not a fatal error: %v", err)
		}
		if !strings.Contains(errOut.String(), "shutdown drain did not finish") {
			t.Fatalf("missing drain warning:\n%s", errOut.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for lifecycle to finish")
	}
}

// TestRunLifecycleClosesStreamsBeforeDrain is the #1110 regression: an open
// SSE-style connection (handler returns only when its stream channel closes)
// must not hold the shutdown hostage for the whole drain timeout — the
// reloader's CloseStreams hook runs first, so Shutdown finishes promptly and
// returns nil.
func TestRunLifecycleClosesStreamsBeforeDrain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	streamClosed := make(chan struct{})
	entered := make(chan struct{})
	server := &http.Server{
		Addr: listener.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-streamClosed:
			case <-r.Context().Done():
			}
		}),
	}

	lifecycleListenAndServe = func(srv *http.Server) error {
		return srv.Serve(listener)
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	reloader := &fakeReloader{closeStreamsHook: func() { close(streamClosed) }}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunLifecycle(LifecycleOptions{
			Context:       ctx,
			Out:           &bytes.Buffer{},
			Err:           &bytes.Buffer{},
			Server:        server,
			StateReloader: reloader,
			DrainTimeout:  5 * time.Second,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	resp, err := http.Get("http://" + listener.Addr().String() + "/")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not start")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("shutdown with an open stream must still succeed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked behind the open stream")
	}
	if got := atomic.LoadInt32(&reloader.closeStreamsCalled); got != 1 {
		t.Fatalf("CloseStreams calls=%d, want 1", got)
	}
}

func TestRunLifecycleTLSError(t *testing.T) {
	wantErr := errors.New("tls boom")
	lifecycleListenAndServeTLS = func(srv *http.Server, certFile, keyFile string) error { return wantErr }
	defer func() {
		lifecycleListenAndServeTLS = func(srv *http.Server, certFile, keyFile string) error {
			return srv.ListenAndServeTLS(certFile, keyFile)
		}
	}()

	err := RunLifecycle(LifecycleOptions{
		Context:    context.Background(),
		Out:        &bytes.Buffer{},
		Err:        &bytes.Buffer{},
		Server:     &http.Server{Addr: "127.0.0.1:0"},
		TLSEnabled: true,
		TLSCert:    "/tmp/cert.pem",
		TLSKey:     "/tmp/key.pem",
	})
	if err == nil || !strings.Contains(err.Error(), "server error") {
		t.Fatalf("expected TLS server error, got %v", err)
	}
}
