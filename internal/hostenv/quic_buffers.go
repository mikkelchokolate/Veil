package hostenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

const (
	// QUICUDPBufferBytes is Hysteria's recommended 16 MiB UDP socket cap. It is
	// a floor: a host already tuned above it keeps its higher limits (#1456).
	QUICUDPBufferBytes = 16777216
	quicSysctlPath     = "/etc/sysctl.d/99-veil-quic.conf"
	quicRMEMSysctlPath = "/proc/sys/net/core/rmem_max"
	quicWMEMSysctlPath = "/proc/sys/net/core/wmem_max"
)

// QUICSysctlContent is the persistent sysctl snippet shipped with Veil.
func QUICSysctlContent() string {
	return QUICSysctlContentFor(QUICUDPBufferBytes, QUICUDPBufferBytes)
}

// QUICSysctlContentFor renders the drop-in for the given caps. Callers pass the
// observed live values raised to at least QUICUDPBufferBytes, so the file never
// lowers a limit the operator tuned higher (#1456).
func QUICSysctlContentFor(rmemMax, wmemMax int) string {
	return fmt.Sprintf(`# Hysteria2/QUIC needs large UDP socket buffers. Linux defaults (~208 KiB)
# drop inbound datagrams and cap client upload around 5–10 Mbit/s while
# download (server send path) can still look fine.
# Values are floored at 16 MiB and never lower an existing higher limit
# (issue #1456).
net.core.rmem_max = %d
net.core.wmem_max = %d
`, rmemMax, wmemMax)
}

var (
	quicGeteuid     = os.Geteuid
	quicReadFile    = os.ReadFile
	quicWriteFile   = os.WriteFile
	quicMkdirAll    = os.MkdirAll
	quicLookPath    = syspath.LookPath
	quicCommand     = syspath.Command
	quicRuntimeGOOS = func() string { return runtime.GOOS }
	quicGlob        = filepath.Glob
	// quicSysctlDirs is the systemd sysctl file set in systemd-sysctl(8)
	// precedence order, highest first: /etc overrides /run, /run overrides
	// /usr/local/lib, /usr/local/lib overrides /usr/lib, /usr/lib overrides
	// /lib. Operator tuning lives here (90-network-tuning.conf and friends), and
	// Veil's own drop-in sorts late, so the files have to be read instead of
	// trusting the live value (#1492).
	quicSysctlDirs = []string{
		"/etc/sysctl.d",
		"/run/sysctl.d",
		"/usr/local/lib/sysctl.d",
		"/usr/lib/sysctl.d",
		"/lib/sysctl.d",
	}
	// quicSysctlConf is applied by systemd-sysctl after every *.conf file, so a
	// tuning written there outranks the drop-ins; the scan reads it too.
	quicSysctlConf = "/etc/sysctl.conf"
)

// operatorQUICBuffer returns the highest explicit cap for key across the systemd
// sysctl file set, or 0 when no file sets it. Files are visited in systemd's
// precedence order and a file shadowed by a higher-precedence directory with the
// same name is skipped, so a deliberate lower value in /etc still wins over the
// vendor copy while a tuned value beats Veil's own late-sorting drop-in (#1492).
func operatorQUICBuffer(key string) int {
	best := 0
	seen := map[string]bool{}
	for _, dir := range quicSysctlDirs {
		matches, err := quicGlob(filepath.Join(dir, "*.conf"))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, path := range matches {
			name := filepath.Base(path)
			if seen[name] {
				continue
			}
			seen[name] = true
			if value := quicBufferFromFile(path, key); value > best {
				best = value
			}
		}
	}
	if value := quicBufferFromFile(quicSysctlConf, key); value > best {
		best = value
	}
	return best
}

// quicBufferFromFile returns the highest value assigned to key in one sysctl
// file, or 0. Both `key = value` and `key=value` are accepted, as is the `-`
// prefix systemd uses to ignore a failing write; comments are skipped.
func quicBufferFromFile(path, key string) int {
	body, err := quicReadFile(path)
	if err != nil {
		return 0
	}
	best := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		line = strings.TrimPrefix(line, "-")
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 0 {
			continue
		}
		if parsed > best {
			best = parsed
		}
	}
	return best
}

func maxQUICBuffer(values ...int) int {
	best := 0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

// observedQUICBuffer returns the live cap at path, or 0 when it cannot be read
// or parsed: unknown means "apply the floor", never "assume it is high".
func observedQUICBuffer(path string) int {
	body, err := quicReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// ApplyQUICUDPBuffers writes the sysctl snippet and raises live UDP buffer
// caps. It only ever raises: a host already tuned above QUICUDPBufferBytes
// keeps its limits, and the drop-in is rewritten to that higher value so the
// next boot does not lower them either (#1456). No-op when not root or not
// Linux so unit tests stay hermetic.
func ApplyQUICUDPBuffers() error {
	if quicRuntimeGOOS() != "linux" || quicGeteuid() != 0 {
		return nil
	}
	if err := quicMkdirAll("/etc/sysctl.d", 0o755); err != nil {
		return fmt.Errorf("create sysctl.d: %w", err)
	}
	liveRMEM := observedQUICBuffer(quicRMEMSysctlPath)
	liveWMEM := observedQUICBuffer(quicWMEMSysctlPath)
	// The live value may already be clamped by Veil's own drop-in, so a host
	// tuned in 90-network-tuning.conf never shows its intent through /proc.
	// Take the highest explicit sysctl.d value as well, and never go below it.
	rmem := maxQUICBuffer(liveRMEM, operatorQUICBuffer("net.core.rmem_max"), QUICUDPBufferBytes)
	wmem := maxQUICBuffer(liveWMEM, operatorQUICBuffer("net.core.wmem_max"), QUICUDPBufferBytes)
	if err := quicWriteFile(quicSysctlPath, []byte(QUICSysctlContentFor(rmem, wmem)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", quicSysctlPath, err)
	}
	raise := make([]string, 0, 2)
	if liveRMEM < rmem {
		raise = append(raise, fmt.Sprintf("net.core.rmem_max=%d", rmem))
	}
	if liveWMEM < wmem {
		raise = append(raise, fmt.Sprintf("net.core.wmem_max=%d", wmem))
	}
	if len(raise) == 0 {
		return nil
	}
	if _, err := quicLookPath("sysctl"); err != nil {
		return fmt.Errorf("sysctl not found for live QUIC buffer tuning: %w", err)
	}
	var errs []error
	for _, spec := range raise {
		cmd := quicCommand("sysctl", "-w", spec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(output))
			if msg == "" {
				errs = append(errs, fmt.Errorf("sysctl -w %s: %w", spec, err))
			} else {
				errs = append(errs, fmt.Errorf("sysctl -w %s: %w (%s)", spec, err, msg))
			}
		}
	}
	return errors.Join(errs...)
}
