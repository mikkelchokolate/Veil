package hostaccess

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Before #1240 the IP-certificate flow ran `acme.sh --install` as root with
// HOME=<VarDir>/acme and without --no-cron. That registered a root crontab
// entry of the form
//
//	12 3 * * * "<VarDir>/acme/.acme.sh"/acme.sh --cron --home "<VarDir>/acme/.acme.sh" > /dev/null
//
// and left the script inside the service-writable VarDir. Cron execs that
// path directly as root every day, so a compromised veil uid could swap the
// script and gain root on the next tick, bypassing the trustedAcmeSh gate and
// the /etc/veil/acme relocation entirely (#1245). Upgrades therefore have to
// remove the entry and take the legacy tree away from the service uid.

// legacyAcmeHomeName is the pre-#1240 acme.sh HOME under VarDir.
const legacyAcmeHomeName = "acme"

// RootCrontab reads and replaces root's crontab. Read reports exists=false
// when root has no crontab or no crontab tool is installed.
type RootCrontab interface {
	Read() (content string, exists bool, err error)
	Write(content string) error
}

// crontabCandidates are the fixed locations of the crontab tool. The binary
// is never resolved through PATH: Prepare runs as root from packaging hooks
// and the installer (#1262).
var crontabCandidates = []string{"/usr/bin/crontab", "/bin/crontab"}

type systemRootCrontab struct{}

func (systemRootCrontab) binary() string {
	for _, candidate := range crontabCandidates {
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func (c systemRootCrontab) Read() (string, bool, error) {
	bin := c.binary()
	if bin == "" {
		return "", false, nil
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, "-u", "root", "-l")
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && strings.Contains(strings.ToLower(stderr.String()+stdout.String()), "no crontab") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read root crontab: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), true, nil
}

func (c systemRootCrontab) Write(content string) error {
	bin := c.binary()
	if bin == "" {
		return fmt.Errorf("write root crontab: crontab tool not found")
	}
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "-u", "root", "-")
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write root crontab: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// legacyAcmeCronLine reports whether a crontab line runs the acme.sh that
// the pre-#1240 installer placed under legacyHome. Quotes are dropped before
// matching because acme.sh quotes its working directory in the entry.
// Comments and entries for any other acme.sh install are left alone.
func legacyAcmeCronLine(line, legacyHome string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	unquoted := strings.NewReplacer(`"`, "", `'`, "").Replace(trimmed)
	marker := filepath.Clean(legacyHome) + "/.acme.sh"
	for offset := 0; ; {
		idx := strings.Index(unquoted[offset:], marker)
		if idx < 0 {
			return false
		}
		begin := offset + idx
		end := begin + len(marker)
		startsToken := begin == 0 || strings.ContainsRune(" \t=;&|(", rune(unquoted[begin-1]))
		endsComponent := end == len(unquoted) || strings.ContainsRune("/ \t;&|)", rune(unquoted[end]))
		if startsToken && endsComponent {
			return true
		}
		offset = end
	}
}

// StripLegacyAcmeCron removes every root crontab entry that execs the
// legacy acme.sh under varDir (#1245) and returns how many were removed.
// Other entries, comments and blank lines are kept byte for byte, and the
// crontab is not rewritten when nothing matched. It is idempotent.
func StripLegacyAcmeCron(cron RootCrontab, varDir string) (int, error) {
	if varDir == "" {
		return 0, fmt.Errorf("var directory is required")
	}
	content, exists, err := cron.Read()
	if err != nil || !exists {
		return 0, err
	}
	legacyHome := filepath.Join(varDir, legacyAcmeHomeName)
	lines := strings.SplitAfter(content, "\n")
	kept := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		if legacyAcmeCronLine(strings.TrimRight(line, "\r\n"), legacyHome) {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := cron.Write(strings.Join(kept, "")); err != nil {
		return 0, err
	}
	return removed, nil
}

// lockDownLegacyAcmeHome takes the pre-#1240 acme.sh tree away from the
// service uid: root-owned, directories 0700, files 0600. Dropping the exec
// bit means even a cron entry this pass did not recognise can no longer run
// a veil-controlled script as root. A missing tree is fine. A symlink is not
// followed or touched; the cron entry that would exec through it is removed
// by StripLegacyAcmeCron, and the planted link stays for the operator.
func lockDownLegacyAcmeHome(varDir string, rootUID, rootGID int) error {
	legacyHome := filepath.Join(varDir, legacyAcmeHomeName)
	info, err := testHooks.lstat(legacyHome)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return err
	case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
		return nil
	}
	return applyTreeOwnership(legacyHome, 0o700, 0o600, rootUID, rootGID, false)
}
