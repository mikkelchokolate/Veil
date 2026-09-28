package caddycapabilities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"time"
)

type CaddyCapabilities struct {
	ForwardProxy bool
	HTTP3        bool
	H3Only       bool
}

type caddyModule struct {
	Name string `json:"module_name"`
}

// probeTimeout bounds `caddy list-modules`: an unbounded probe could wedge the
// apply plan builder (and with it the management API) on a hung caddy binary
// (#1141).
const probeTimeout = 10 * time.Second

// probeMaxOutput caps how much stdout `caddy list-modules --json` may emit. A
// module list is small; anything larger is a broken/binary-masquerading
// executable and must not be allowed to exhaust panel memory (#1141).
const probeMaxOutput = 4 << 20 // 4 MiB

var errProbeOutputTooLarge = errors.New("caddy list-modules output exceeded size cap")

// cappedWriter truncates writes at limit and reports the overflow through
// exceeded so the caller can reject oversized probe output instead of
// buffering it unboundedly.
type cappedWriter struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining < len(p) {
		w.exceeded = true
		if remaining > 0 {
			_, _ = w.buf.Write(p[:remaining])
		}
		return len(p), nil
	}
	return w.buf.Write(p)
}

// IsMissingBinary reports whether Probe failed because no Caddy executable
// was on PATH (or the given path does not exist). Panel-only install renders
// Caddy JSON before veil runtime install has placed the binary.
func IsMissingBinary(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// Probe runs `caddy list-modules --json` with a bounded timeout and capped
// output. Missing-binary errors stay unwrapped enough for IsMissingBinary.
func Probe(binaryPath string) (CaddyCapabilities, error) {
	return ProbeContext(context.Background(), binaryPath)
}

// ProbeContext is Probe bound to a caller context; the internal timeout still
// applies so an unresponsive caddy can never outlive it.
func ProbeContext(ctx context.Context, binaryPath string) (CaddyCapabilities, error) {
	if binaryPath == "" {
		binaryPath = "caddy"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "list-modules", "--json")
	stdout := &cappedWriter{limit: probeMaxOutput}
	stderr := &cappedWriter{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return CaddyCapabilities{}, fmt.Errorf("caddy list-modules exceeded %s: %w", probeTimeout, ctx.Err())
	}
	if stdout.exceeded || stderr.exceeded {
		return CaddyCapabilities{}, fmt.Errorf("caddy list-modules failed: %w", errProbeOutputTooLarge)
	}
	if runErr != nil {
		return CaddyCapabilities{}, fmt.Errorf("caddy list-modules failed: %w", runErr)
	}
	// Route production probing through the same parser the tests lock, so a
	// parseModuleList regression cannot diverge from live behavior (#882).
	return parseModuleList(stdout.buf.Bytes())
}

func hasModule(modules []caddyModule, name string) bool {
	for _, m := range modules {
		if m.Name == name {
			return true
		}
	}
	return false
}

func parseModules(data []byte) ([]caddyModule, error) {
	var modules []caddyModule
	if err := json.Unmarshal(data, &modules); err != nil {
		return nil, err
	}
	return modules, nil
}

func parseModuleList(data []byte) (CaddyCapabilities, error) {
	modules, err := parseModules(data)
	if err != nil {
		return CaddyCapabilities{}, err
	}
	caps := CaddyCapabilities{
		ForwardProxy: hasModule(modules, "http.handlers.forward_proxy"),
	}
	// HTTP3 is available in standard Caddy builds; this flag is set true when
	// the base http app module is present. H3Only is intentionally left false
	// here and verified behaviorally before `quic` transport is accepted.
	caps.HTTP3 = hasModule(modules, "http")
	return caps, nil
}
