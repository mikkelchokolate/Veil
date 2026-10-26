package hostenv

import (
	"errors"
	"fmt"
	"os"
	"runtime"
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
)

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
	rmem := liveRMEM
	if rmem < QUICUDPBufferBytes {
		rmem = QUICUDPBufferBytes
	}
	wmem := liveWMEM
	if wmem < QUICUDPBufferBytes {
		wmem = QUICUDPBufferBytes
	}
	if err := quicWriteFile(quicSysctlPath, []byte(QUICSysctlContentFor(rmem, wmem)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", quicSysctlPath, err)
	}
	raise := make([]string, 0, 2)
	if liveRMEM < QUICUDPBufferBytes {
		raise = append(raise, fmt.Sprintf("net.core.rmem_max=%d", QUICUDPBufferBytes))
	}
	if liveWMEM < QUICUDPBufferBytes {
		raise = append(raise, fmt.Sprintf("net.core.wmem_max=%d", QUICUDPBufferBytes))
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
