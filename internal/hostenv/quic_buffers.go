package hostenv

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

const (
	// QUICUDPBufferBytes is Hysteria's recommended 16 MiB UDP socket cap.
	QUICUDPBufferBytes = 16777216
	quicSysctlPath     = "/etc/sysctl.d/99-veil-quic.conf"
)

// QUICSysctlContent is the persistent sysctl snippet shipped with Veil.
func QUICSysctlContent() string {
	return `# Hysteria2/QUIC needs large UDP socket buffers. Linux defaults (~208 KiB)
# drop inbound datagrams and cap client upload around 5–10 Mbit/s while
# download (server send path) can still look fine.
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
`
}

var (
	quicGeteuid     = os.Geteuid
	quicWriteFile   = os.WriteFile
	quicMkdirAll    = os.MkdirAll
	quicLookPath    = exec.LookPath
	quicCommand     = exec.Command
	quicRuntimeGOOS = func() string { return runtime.GOOS }
)

// ApplyQUICUDPBuffers writes the sysctl snippet and raises live UDP buffer
// caps. No-op when not root or not Linux so unit tests stay hermetic.
func ApplyQUICUDPBuffers() error {
	if quicRuntimeGOOS() != "linux" || quicGeteuid() != 0 {
		return nil
	}
	if err := quicMkdirAll("/etc/sysctl.d", 0o755); err != nil {
		return fmt.Errorf("create sysctl.d: %w", err)
	}
	if err := quicWriteFile(quicSysctlPath, []byte(QUICSysctlContent()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", quicSysctlPath, err)
	}
	if _, err := quicLookPath("sysctl"); err != nil {
		return nil
	}
	for _, spec := range []string{
		fmt.Sprintf("net.core.rmem_max=%d", QUICUDPBufferBytes),
		fmt.Sprintf("net.core.wmem_max=%d", QUICUDPBufferBytes),
	} {
		cmd := quicCommand("sysctl", "-w", spec)
		_ = cmd.Run()
	}
	return nil
}
