package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestReadinessTreatsProtocolRuntimesAsOptional(t *testing.T) {
	readiness := NewReadiness("test", func(name string) (string, error) {
		if name == "systemctl" {
			return "/bin/systemctl", nil
		}
		return "", errors.New("missing")
	})
	summary := readiness.Summary()
	if !summary.Ready {
		t.Fatalf("summary should be ready when only optional commands are missing: %+v", summary)
	}
}

func TestReadinessReportsNotReadyWhenRequiredMissing(t *testing.T) {
	readiness := NewReadiness("test", func(name string) (string, error) {
		return "", errors.New("not found")
	})
	summary := readiness.Summary()
	if summary.Ready {
		t.Fatalf("summary should not be ready when required command is missing: %+v", summary)
	}
	if len(summary.Commands) != 1+6 {
		t.Fatalf("expected 7 command statuses, got %d", len(summary.Commands))
	}
	if summary.Commands[0].Name != "systemctl" || summary.Commands[0].Present {
		t.Fatalf("expected systemctl to be missing: %+v", summary.Commands[0])
	}
}

func TestReadinessReportsOptionalPresent(t *testing.T) {
	readiness := NewReadiness("test", func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	})
	summary := readiness.Summary()
	if !summary.Ready {
		t.Fatalf("summary should be ready when all commands are present: %+v", summary)
	}
	for _, command := range summary.Commands {
		if !command.Present {
			t.Fatalf("expected %s to be present: %+v", command.Name, command)
		}
		want := "/usr/bin/" + command.Name
		if command.Path != want {
			t.Fatalf("expected path %q for %s, got %q", want, command.Name, command.Path)
		}
	}
}

func TestReadinessRuntimeReflectsCurrentPlatform(t *testing.T) {
	readiness := NewReadiness("test", func(name string) (string, error) {
		return "/bin/" + name, nil
	})
	summary := readiness.Summary()
	want := runtime.GOOS + "/" + runtime.GOARCH
	if summary.Runtime != want {
		t.Fatalf("expected Runtime %q, got %q", want, summary.Runtime)
	}
}

func TestPresentationRendersHumanSummary(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []CommandStatus{{Name: "systemctl", Path: "/bin/systemctl", Present: true}}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Veil doctor", "Version: test", "Ready: yes", "- systemctl: /bin/systemctl"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPresentationRendersJSONSummary(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []CommandStatus{{Name: "systemctl", Path: "/bin/systemctl", Present: true}}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, true); err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded Summary
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v\n%s", err, out.String())
	}
	if decoded.Version != summary.Version || decoded.Runtime != summary.Runtime || decoded.Ready != summary.Ready {
		t.Fatalf("decoded summary mismatch: got %+v, want %+v", decoded, summary)
	}
	if len(decoded.Commands) != len(summary.Commands) {
		t.Fatalf("expected %d commands, got %d", len(summary.Commands), len(decoded.Commands))
	}
}

func TestPresentationRendersNotReady(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: false, Commands: []CommandStatus{{Name: "systemctl", Present: false, Error: "not found"}}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Ready: no", "- systemctl: missing (not found)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPresentationRendersMissingRequiredWithoutError(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: false, Commands: []CommandStatus{{Name: "systemctl", Present: false}}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "- systemctl: missing\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

func TestPresentationRendersOptionalCommands(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []CommandStatus{
		{Name: "systemctl", Path: "/bin/systemctl", Present: true},
		{Name: "caddy", Path: "/usr/bin/caddy", Present: true, Optional: true},
		{Name: "hysteria", Present: false, Optional: true},
	}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Optional commands:", "- caddy: /usr/bin/caddy", "- hysteria: missing (warning)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPresentationRendersNoOptionalCommands(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []CommandStatus{
		{Name: "systemctl", Path: "/bin/systemctl", Present: true},
	}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "- none"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}
