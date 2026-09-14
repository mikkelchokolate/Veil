package caddycapabilities

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
)

type CaddyCapabilities struct {
	ForwardProxy bool
	HTTP3        bool
	H3Only       bool
}

type caddyModule struct {
	Name string `json:"module_name"`
}

// IsMissingBinary reports whether Probe failed because no Caddy executable
// was on PATH (or the given path does not exist). Panel-only install renders
// Caddy JSON before veil runtime install has placed the binary.
func IsMissingBinary(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

func Probe(binaryPath string) (CaddyCapabilities, error) {
	if binaryPath == "" {
		binaryPath = "caddy"
	}
	out, err := exec.Command(binaryPath, "list-modules", "--json").Output()
	if err != nil {
		return CaddyCapabilities{}, fmt.Errorf("caddy list-modules failed: %w", err)
	}
	modules, err := parseModules(out)
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
	var caps CaddyCapabilities
	for _, m := range modules {
		switch m.Name {
		case "http.handlers.forward_proxy":
			caps.ForwardProxy = true
		case "http":
			// HTTP base present
		}
	}
	return caps, nil
}
