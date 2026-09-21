package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

type NaiveUser struct {
	Username string
	Password string
}

type NaiveConfig struct {
	Domain       string
	Email        string
	ListenPort   int
	Username     string
	Password     string
	Users        []NaiveUser
	FallbackRoot string
	// FallbackBase is the managed fallback web-root base (the <etc>/www tree).
	// When empty it resolves from the host environment (VEIL_ETC_DIR and
	// friends) so custom --etc-dir installs serve the tree they provision.
	FallbackBase string
	PanelPort    int
	WebBasePath  string
	Upstream     string
}

func RenderNaiveCaddyfile(cfg NaiveConfig) (string, error) {
	if cfg.Domain == "" {
		return "", errors.New("domain is required")
	}
	if cfg.ListenPort <= 0 {
		return "", errors.New("listen port is required")
	}
	if cfg.ListenPort > 65535 {
		return "", errors.New("listen port must be between 1 and 65535")
	}
	if len(cfg.Users) == 0 {
		if cfg.Username == "" || cfg.Password == "" {
			return "", errors.New("naive username and password are required")
		}
		cfg.Users = []NaiveUser{{Username: cfg.Username, Password: cfg.Password}}
	}
	for _, user := range cfg.Users {
		if user.Username == "" || user.Password == "" {
			return "", errors.New("naive username and password are required")
		}
	}
	fallbackBase := cfg.FallbackBase
	if fallbackBase == "" {
		fallbackBase = naiveFallbackBase()
	}
	fallbackBase = filepath.ToSlash(filepath.Clean(fallbackBase))
	if cfg.FallbackRoot == "" {
		cfg.FallbackRoot = fallbackBase
	}
	cfg.FallbackRoot = filepath.Clean(cfg.FallbackRoot)
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/") {
		for _, seg := range strings.Split(filepath.ToSlash(cfg.FallbackRoot), "/") {
			if seg == ".." {
				return "", fmt.Errorf("fallback root must not contain '..' path traversal: %s", cfg.FallbackRoot)
			}
		}
		cfg.FallbackRoot = filepath.Clean(fallbackBase + "/" + cfg.FallbackRoot)
	}
	if !naiveFallbackRootAllowedUnder(filepath.ToSlash(cfg.FallbackRoot), fallbackBase) {
		return "", fmt.Errorf("fallback root must be within %s: %s", fallbackBase, cfg.FallbackRoot)
	}
	cfg.FallbackRoot = filepath.ToSlash(cfg.FallbackRoot)

	// Keep this layout aligned with the NaiveProxy upstream server example.
	// In particular, :port must be the first site address and production must
	// never silently fall back to an internally-issued certificate: Chromium-
	// based Naive clients require a publicly trusted TLS chain.
	const tpl = `{
  order forward_proxy before file_server
  log {
    exclude http.log.error
  }
  servers {
    protocols h1 h2
  }
}

:{{ .ListenPort }}, {{ .Domain }} {
  tls {
    issuer acme{{ if .Email }} {
      email {{ .Email }}
    }{{ end }}
  }
  encode

  forward_proxy {
{{- range .Users }}
    basic_auth {{ .Username }} {{ .Password }}
{{- end }}
    hide_ip
    hide_via
    probe_resistance
{{- if .Upstream }}
    upstream {{ .Upstream }}
{{- end }}
  }

{{- if .PanelPort }}
{{- if .WebBasePath }}
  handle {{ .WebBasePath }}* {
    reverse_proxy 127.0.0.1:{{ .PanelPort }}
  }
{{- end }}
{{- end }}

  root * {{ .FallbackRoot }}
  file_server
}
`
	var out bytes.Buffer
	if err := template.Must(template.New("caddy").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}

// NaiveDefaultFallbackRoot is the packaged web root Caddy serves for naive
// fallback. It lives under /etc/veil because the caddy unit runs as
// veil-proxy with /var/lib/veil in InaccessiblePaths: a var-lib root would
// be unreachable (audit #497). Custom --etc-dir installs resolve their own
// <etc>/www tree through naiveFallbackBase/hostenv.EtcDir instead.
const NaiveDefaultFallbackRoot = "/etc/veil/www"

// naiveFallbackBase resolves the fallback web root for this process:
// <etc>/www where <etc> honors VEIL_ETC_DIR/VEIL_LIVE_ROOT/VEIL_KEY_PATH and
// defaults to /etc/veil.
func naiveFallbackBase() string {
	return filepath.ToSlash(filepath.Join(hostenv.EtcDir(), "www"))
}

// NaiveFallbackRootAllowed enforces the fallback-root boundary against the
// host's resolved <etc>/www tree: exactly that root or a subdirectory (never
// the etc root itself or siblings like panel/, which hold keys readable by
// veil-proxy). The legacy /var/lib/veil subtree is no longer accepted: the
// caddy unit masks it via InaccessiblePaths, so a root there silently serves
// nothing (issues #618, #634).
func NaiveFallbackRootAllowed(root string) bool {
	return naiveFallbackRootAllowedUnder(root, naiveFallbackBase())
}

// naiveFallbackRootAllowedUnder reports whether root is exactly base or a
// subdirectory of it. Both arguments are expected to be slash-normalized
// absolute paths.
func naiveFallbackRootAllowedUnder(root, base string) bool {
	return root == base || strings.HasPrefix(root, base+"/")
}
