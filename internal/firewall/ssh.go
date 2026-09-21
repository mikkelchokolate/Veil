package firewall

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	sshConfigPaths = []string{"/etc/ssh/sshd_config"}
	sshConfigGlob  = "/etc/ssh/sshd_config.d/*.conf"
	// sshConfigBaseDir is where Include directives resolve non-absolute
	// paths, per sshd_config(5): "Files without absolute paths are assumed
	// to be in /etc/ssh".
	sshConfigBaseDir = "/etc/ssh"
	// sshSocketUnitDirs are the systemd unit directories searched for
	// socket-activated SSH units and their drop-ins. Detection unions every
	// unit it finds — an extra opening is harmless next to a missed real
	// port, which is the lockout failure mode (#633).
	sshSocketUnitDirs = []string{
		"/etc/systemd/system",
		"/run/systemd/system",
		"/usr/lib/systemd/system",
		"/lib/systemd/system",
	}
	sshSocketUnits = []string{"ssh.socket", "sshd.socket"}
	readSSHFile    = os.ReadFile
	globSSHFiles   = filepath.Glob
)

const (
	// sshIncludeMaxDepth bounds recursive Include expansion so a self-
	// including glob or drop-in cycle cannot spin forever.
	sshIncludeMaxDepth = 8
	// sshConfigMaxFiles bounds the total parsed file set so a broad Include
	// glob cannot blow detection up.
	sshConfigMaxFiles = 128
)

// DetectSSHPorts returns the host SSH listen ports that must stay reachable
// before UFW is enabled. It unions Port/ListenAddress ports from the main
// sshd_config, the drop-in glob, and every transitively Included file
// (#616), plus ListenStream/ListenDatagram ports from systemd ssh.socket
// units (#633). It defaults to 22 when no source declares a port.
func DetectSSHPorts() []int {
	seen := map[int]bool{}
	var ports []int
	add := func(found []int) {
		for _, port := range found {
			if seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, port)
		}
	}
	for _, data := range sshConfigFileContents() {
		add(parseSSHConfigPorts(data))
	}
	for _, data := range sshSocketFileContents() {
		add(parseSSHSocketPorts(data))
	}
	if len(ports) == 0 {
		return []int{22}
	}
	return ports
}

// sshConfigFileContents returns the sshd_config file set: the main paths and
// drop-in glob plus every Include target found transitively. Include is
// followed even inside Match blocks — conditional inclusion is rare, and
// unioning a port sshd does not actually bind is harmless next to missing
// the real one (which is the lockout failure mode).
func sshConfigFileContents() [][]byte {
	var contents [][]byte
	visited := map[string]bool{}
	var visit func(path string, depth int)
	visit = func(path string, depth int) {
		if depth > sshIncludeMaxDepth || len(contents) >= sshConfigMaxFiles {
			return
		}
		key := filepath.Clean(path)
		if visited[key] {
			return
		}
		visited[key] = true
		data, err := readSSHFile(path)
		if err != nil {
			return
		}
		contents = append(contents, data)
		for _, include := range parseSSHConfigIncludes(data) {
			resolved := include
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(sshConfigBaseDir, resolved)
			}
			matches, err := globSSHFiles(resolved)
			if err != nil {
				continue
			}
			sort.Strings(matches)
			for _, match := range matches {
				visit(match, depth+1)
			}
		}
	}
	for _, path := range sshConfigPaths {
		visit(path, 0)
	}
	if sshConfigGlob != "" {
		if matches, err := globSSHFiles(sshConfigGlob); err == nil {
			sort.Strings(matches)
			for _, match := range matches {
				visit(match, 0)
			}
		}
	}
	return contents
}

// sshSocketFileContents reads every ssh.socket/sshd.socket unit and its
// .d/*.conf drop-ins across the systemd unit directories. Units that are not
// installed simply do not exist and are skipped; the union deliberately errs
// toward extra ports because a dormant socket's port costs one needless
// allow while a missed live port locks the operator out.
func sshSocketFileContents() [][]byte {
	var contents [][]byte
	seen := map[string]bool{}
	add := func(path string) {
		key := filepath.Clean(path)
		if seen[key] {
			return
		}
		seen[key] = true
		if data, err := readSSHFile(path); err == nil {
			contents = append(contents, data)
		}
	}
	for _, dir := range sshSocketUnitDirs {
		for _, unit := range sshSocketUnits {
			add(filepath.Join(dir, unit))
		}
	}
	for _, dir := range sshSocketUnitDirs {
		for _, unit := range sshSocketUnits {
			matches, err := globSSHFiles(filepath.Join(dir, unit+".d", "*.conf"))
			if err != nil {
				continue
			}
			sort.Strings(matches)
			for _, match := range matches {
				add(match)
			}
		}
	}
	return contents
}

// parseSSHConfigIncludes returns every pathname argument of the file's
// Include directives; each Include may list multiple glob paths.
func parseSSHConfigIncludes(data []byte) []string {
	var includes []string
	for _, raw := range strings.Split(string(data), "\n") {
		keyword, args, ok := sshConfigEntry(raw)
		if !ok || !strings.EqualFold(keyword, "Include") {
			continue
		}
		includes = append(includes, args...)
	}
	return includes
}

// sshConfigEntry splits an sshd_config line into keyword and arguments.
// OpenSSH separates the keyword by whitespace or an optional '=' (so
// "Port=2222" parses the same as "Port 2222"); blank and comment lines
// report ok=false.
func sshConfigEntry(line string) (keyword string, args []string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil, false
	}
	keyword = line
	rest := ""
	for i := 0; i < len(line); i++ {
		if c := line[i]; c == ' ' || c == '\t' || c == '=' {
			keyword = line[:i]
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line[i:]), "="))
			break
		}
	}
	if args = strings.Fields(rest); len(args) == 0 {
		return "", nil, false
	}
	return keyword, args, true
}

func parseSSHConfigPorts(data []byte) []int {
	var ports []int
	seen := map[int]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		keyword, args, ok := sshConfigEntry(raw)
		if !ok {
			continue
		}
		var port int
		var valid bool
		switch {
		case strings.EqualFold(keyword, "Port"):
			port, valid = sshPortValue(args[0])
		case strings.EqualFold(keyword, "ListenAddress"):
			// sshd also binds ports via ListenAddress host:port and
			// [addr]:port forms — commonly with no Port directive at all.
			// Missing those ports would plan a UFW SSH allow for 22 while
			// sshd only listens on the ListenAddress port, locking the
			// operator out. Bare addresses without a port are skipped: they
			// bind the Port directives, which are already collected.
			port, valid = sshListenAddressPort(args[0])
		default:
			continue
		}
		if !valid || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// parseSSHSocketPorts extracts ListenStream/ListenDatagram ports from a
// systemd socket unit body. Only the [Socket] section is consulted, so a
// drop-in that touches other sections cannot feed stray ports in.
func parseSSHSocketPorts(data []byte) []int {
	var ports []int
	seen := map[int]bool{}
	inSocketSection := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSocketSection = strings.EqualFold(strings.TrimSpace(line[1:len(line)-1]), "socket")
			continue
		}
		if !inSocketSection {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "listenstream", "listendatagram":
		default:
			continue
		}
		port, ok := sshSocketListenPort(strings.TrimSpace(value))
		if !ok || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// sshSocketListenPort extracts the port from a systemd socket listen value:
// a bare port ("2222"), host:port, or [v6]:port. Unix-socket paths, an empty
// value (the "reset the list" form), and unresolvable names yield no port;
// the service name "ssh" maps to its well-known 22.
func sshSocketListenPort(value string) (int, bool) {
	if value == "" || strings.HasPrefix(value, "/") {
		return 0, false
	}
	if index := strings.LastIndex(value, ":"); index >= 0 {
		value = value[index+1:]
	}
	if port, err := strconv.Atoi(value); err == nil {
		if port <= 0 || port > 65535 {
			return 0, false
		}
		return port, true
	}
	if strings.EqualFold(value, "ssh") {
		return 22, true
	}
	return 0, false
}

func sshPortValue(value string) (int, bool) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// sshListenAddressPort extracts the explicit port from an sshd ListenAddress
// specification: IPv4 host:port, hostname:port, and bracketed IPv6
// [addr]:port forms. Bare addresses (including unbracketed IPv6) carry no
// port and report false.
func sshListenAddressPort(spec string) (int, bool) {
	if !strings.Contains(spec, ":") {
		return 0, false
	}
	_, portText, err := net.SplitHostPort(spec)
	if err != nil {
		return 0, false
	}
	return sshPortValue(portText)
}
