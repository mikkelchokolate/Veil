package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestMain(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil", "--help"}
	defer func() { os.Args = origArgs }()

	origStdout := os.Stdout
	nullOut, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	os.Stdout = nullOut
	defer func() {
		os.Stdout = origStdout
		nullOut.Close()
	}()

	origExit := osExit
	defer func() { osExit = origExit }()

	var called bool
	var exitCode int
	osExit = func(code int) {
		called = true
		exitCode = code
	}

	main()

	if !called {
		t.Fatal("expected osExit to be called")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestMainInvalidCommand(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"veil", "nonexistent"}
	defer func() { os.Args = origArgs }()

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) { exitCode = code }

	main()
	w.Close()

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !bytes.Contains(buf.Bytes(), []byte("veil:")) {
		t.Fatalf("expected stderr to contain 'veil:', got %q", output)
	}
}

func TestRunShutdownOnStdinClose(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "default help command",
			args: []string{"veil"},
		},
		{
			name: "explicit help flag",
			args: []string{"veil", "--help"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VEIL_SHUTDOWN_ON_STDIN_CLOSE", "1")

			origArgs := os.Args
			os.Args = tt.args
			defer func() { os.Args = origArgs }()

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			origStdin := os.Stdin
			os.Stdin = r

			done := make(chan int, 1)
			go func() {
				done <- run()
			}()

			var code int
			select {
			case code = <-done:
			case <-time.After(5 * time.Second):
				w.Close()
				t.Fatal("run did not return in time")
			}

			// Unblock the background stdin watcher goroutine so it exits cleanly.
			w.Close()
			time.Sleep(50 * time.Millisecond)
			r.Close()
			os.Stdin = origStdin

			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
		})
	}
}

func TestHandleErrorWithEmptyMessage(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	handleError("")
	w.Close()

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()
	if output != "veil: \n" {
		t.Fatalf("expected 'veil: \\n', got %q", output)
	}
}
