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
	reloadCalled int32
	reloadErr    error
}

func (f *fakeReloader) Reload() error {
	atomic.AddInt32(&f.reloadCalled, 1)
	return f.reloadErr
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
	lifecycleListenAndServe = func(srv *http.Server) error { return http.ErrServerClosed }
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	// Exercise default Out, Err, Context, and DrainTimeout.
	err := RunLifecycle(LifecycleOptions{Server: &http.Server{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}
}

func TestRunLifecycleShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	server := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }),
	}

	lifecycleListenAndServe = func(srv *http.Server) error {
		return srv.Serve(listener)
	}
	defer func() { lifecycleListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() } }()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunLifecycle(LifecycleOptions{
			Context:      ctx,
			Out:          &bytes.Buffer{},
			Err:          &bytes.Buffer{},
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
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	cancel()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "shutdown error") {
			t.Fatalf("expected shutdown error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for lifecycle to finish")
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
