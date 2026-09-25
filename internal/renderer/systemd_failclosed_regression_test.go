package renderer

import (
	"strings"
	"testing"
)

// TestSystemdQuoteEscapesPercentSpecifiers is the #1001 regression: an
// operator-chosen path containing % would specifier-expand at unit load and
// silently redirect directives, so every literal % must render as %%.
func TestSystemdQuoteEscapesPercentSpecifiers(t *testing.T) {
	if got := systemdQuote("/opt/veil%h/etc"); got != "/opt/veil%%h/etc" {
		t.Fatalf("systemdQuote = %q, want %% escaped", got)
	}
	units := RenderSystemdUnits(SystemdConfig{EtcDir: "/opt/veil%h/etc", VarDir: "/var/lib/veil%t"})
	for name, unit := range units {
		if strings.Contains(unit, "/opt/veil%h/") || strings.Contains(unit, "/var/lib/veil%t") {
			t.Fatalf("unit %s leaves an unescaped %% specifier in an operator path:\n%s", name, unit)
		}
	}
	if !strings.Contains(units[UnitVeil], "/opt/veil%%h/etc/veil.env") {
		t.Fatalf("veil.service must carry the escaped path:\n%s", units[UnitVeil])
	}
}

// TestSystemdQuotePreservesInstanceSpecifier locks the template-unit carve
// out of the #1001 escape: veil-hysteria2@.service/veil-olcrtc@.service
// resolve %i to the instance name — escaping it would point the units at a
// literal "%i.yaml" file.
func TestSystemdQuotePreservesInstanceSpecifier(t *testing.T) {
	// A non-specifier % in the operator-chosen dir is escaped (%%h); the
	// render's own %i.yaml stays a specifier.
	got := systemdQuoteInstanceConfig("/opt/veil%h/etc/generated/hysteria2/%i.yaml")
	want := "/opt/veil%%h/etc/generated/hysteria2/%i.yaml"
	if got != want {
		t.Fatalf("systemdQuoteInstanceConfig = %q, want %q", got, want)
	}
}

// TestValidateSystemdConfigRejectsControlCharacters is the #1001 fail-closed
// half: a newline, carriage return, or NUL in any rendered path would break
// out of its directive line and inject arbitrary unit directives.
func TestValidateSystemdConfigRejectsControlCharacters(t *testing.T) {
	for _, cfg := range []SystemdConfig{
		{EtcDir: "/etc/veil\nExecStart=/bin/sh"},
		{VarDir: "/var/lib/veil\r\n[Install]"},
		{VeilBinary: "/usr/local/bin/veil\x00evil"},
		{CaddyBinary: "/bin/caddy\n"},
		{MieruBinary: "/bin/mita\r"},
	} {
		if err := ValidateSystemdConfig(cfg); err == nil {
			t.Fatalf("ValidateSystemdConfig(%+v) must reject control characters", cfg)
		}
	}
	if err := ValidateSystemdConfig(SystemdConfig{}); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
	if err := ValidateSystemdConfig(SystemdConfig{EtcDir: "/etc/veil", VarDir: "/var/lib/veil"}); err != nil {
		t.Fatalf("normal config must validate: %v", err)
	}
}

// TestMieruActivationExecStartPostPassesPathsAsArgv is the #1024 regression:
// the binary and config paths must reach the /bin/sh -c script through argv
// ($$1/$$2 → "$1"/"$2"), never interpolated into the single-quoted script
// text where shell metacharacters inside an operator path would execute.
func TestMieruActivationExecStartPostPassesPathsAsArgv(t *testing.T) {
	line := mieruActivationExecStartPost(systemdQuote("/opt/weird/bin/mita"), systemdQuote("/etc/veil/generated/mieru/server_config.json"))

	// The script text between the single quotes must be a fixed literal with
	// no path material in it.
	script := line[strings.IndexByte(line, '\'')+1 : strings.LastIndexByte(line, '\'')]
	for _, injected := range []string{"server_config", "/opt", "/etc"} {
		if strings.Contains(script, injected) {
			t.Fatalf("activation script must not embed paths; %q leaked into %q", injected, script)
		}
	}
	for _, want := range []string{`"$$1" apply config "$$2"`, `"$$1" start`} {
		if !strings.Contains(script, want) {
			t.Fatalf("activation script missing argv indirection %q: %q", want, script)
		}
	}
	if !strings.Contains(line, "' sh /opt/weird/bin/mita /etc/veil/generated/mieru/server_config.json") {
		t.Fatalf("paths must trail the script as systemd argv words: %q", line)
	}

	// A path carrying shell metacharacters stays one quoted argv word —
	// quoted by the caller (systemdQuote), the script only sees "$1".
	evil := "/opt/`id` dir/mita"
	line = mieruActivationExecStartPost(systemdQuote(evil), systemdQuote("/etc/veil/c.json"))
	if !strings.Contains(line, ` sh "`+evil+`"`) {
		t.Fatalf("metachar path must reach the script as one quoted argv word: %q", line)
	}
	script = line[strings.IndexByte(line, '\'')+1 : strings.LastIndexByte(line, '\'')]
	if strings.Contains(script, "`id`") {
		t.Fatalf("metacharacters must not appear inside the script text: %q", script)
	}
}
