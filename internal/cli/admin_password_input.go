package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// adminPasswordInputMaxBytes bounds --password-stdin / --password-file reads.
// The panel policy caps passwords at 72 bytes; the extra headroom only lets
// the policy check report "too long" instead of a silent truncation.
const adminPasswordInputMaxBytes int64 = 4 * 1024

const adminPasswordArgvWarning = "warning: --password exposes the password to every local user via the process list (/proc/<pid>/cmdline); prefer --password-stdin, --password-file, or the interactive prompt\n"

var errAdminPasswordRequired = errors.New("a password is required: pipe it with --password-stdin, read it with --password-file <path>, or run on a terminal to be prompted")

// adminPasswordTerminal reports whether in is an interactive terminal and,
// when it is, reads one line without echo. Package-level so tests can stand
// in for a TTY.
var adminPasswordTerminal = func(in io.Reader) (func() ([]byte, error), bool) {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, false
	}
	return func() ([]byte, error) { return term.ReadPassword(int(file.Fd())) }, true
}

type adminPasswordSource struct {
	argv     string
	argvSet  bool
	useStdin bool
	file     string
}

// resolveAdminPassword picks exactly one password source for `veil admin
// set`. --password stays accepted for backwards compatibility but warns,
// because argv is world-readable through /proc/<pid>/cmdline (#1261).
func resolveAdminPassword(src adminPasswordSource, stdin io.Reader, stderr io.Writer) (string, error) {
	selected := 0
	for _, set := range []bool{src.argvSet, src.useStdin, src.file != ""} {
		if set {
			selected++
		}
	}
	if selected > 1 {
		return "", errors.New("--password, --password-stdin and --password-file are mutually exclusive")
	}
	switch {
	case src.argvSet:
		fmt.Fprint(stderr, adminPasswordArgvWarning)
		if src.argv == "" {
			return "", errAdminPasswordRequired
		}
		return src.argv, nil
	case src.useStdin:
		body, err := readAdminPasswordLimited(stdin)
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return trimAdminPassword(body)
	case src.file != "":
		file, err := openCLIRegularNoFollow(src.file)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("read password file: not a regular file")
		}
		body, err := readAdminPasswordLimited(file)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		return trimAdminPassword(body)
	}
	readLine, ok := adminPasswordTerminal(stdin)
	if !ok {
		return "", errAdminPasswordRequired
	}
	fmt.Fprint(stderr, "New password: ")
	first, err := readLine()
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password interactively: %w", err)
	}
	fmt.Fprint(stderr, "Confirm password: ")
	second, err := readLine()
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password interactively: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	if len(first) == 0 {
		return "", errAdminPasswordRequired
	}
	return string(first), nil
}

func readAdminPasswordLimited(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("no input")
	}
	body, err := io.ReadAll(io.LimitReader(r, adminPasswordInputMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > adminPasswordInputMaxBytes {
		return nil, fmt.Errorf("input exceeds %d-byte limit", adminPasswordInputMaxBytes)
	}
	return body, nil
}

// trimAdminPassword strips exactly one trailing line ending (as written by
// echo/printf or an editor) and rejects multi-line input, so a stray extra
// line is never silently folded into the credential.
func trimAdminPassword(body []byte) (string, error) {
	value := string(body)
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("password input must be a single line")
	}
	if value == "" {
		return "", errAdminPasswordRequired
	}
	return value, nil
}
