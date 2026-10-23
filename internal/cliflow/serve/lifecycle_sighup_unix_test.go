//go:build unix

package serve

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

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
