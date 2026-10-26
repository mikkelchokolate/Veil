package firewall

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// warnInactiveUFW reports the configured-yet-unloaded ruleset to the operator.
// Tests replace it.
var warnInactiveUFW = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

// configuredSnapshot reads `ufw show added`. It is the only way to see the
// configured ruleset while UFW is inactive: `ufw status` then prints just
// `Status: inactive`, even though the rules exist and become live on the next
// `ufw enable` (#1434). ok is false when ufw is absent or the query fails, so
// callers keep the status-only view instead of erroring.
func (a UFWApplier) configuredSnapshot() (ufwSnapshot, bool) {
	out := a.runUFW(15*time.Second, "show", "added")
	if out.NotFound || out.Err != nil || out.TimedOut {
		return ufwSnapshot{}, false
	}
	return parseUFWShowAdded(out.Output), true
}

// ConfiguredRule is one `ufw show added` line decoded for callers outside this
// package (the privileged reconcile replays these entries on rollback).
type ConfiguredRule struct {
	Target    string
	Action    string
	Direction string
	Source    string
	Comment   string
	// Args is the rule exactly as `ufw show added` printed it (without the
	// `ufw` prefix), so a rollback can replay the original scope instead of
	// re-encoding it and widening the rule (#1434 review).
	Args []string
}

// ParseConfiguredRules decodes `ufw show added` output. Unrecognised lines are
// skipped rather than guessed.
func ParseConfiguredRules(output string) []ConfiguredRule {
	snap := parseUFWShowAdded(output)
	return append([]ConfiguredRule(nil), snap.configured...)
}

// parseUFWShowAdded turns `ufw show added` lines back into the same
// destination→comment snapshot the status parser produces. Only the forms a
// Veil-managed host can meet are decoded: plain ports (`22/tcp`, ranges),
// `port N proto P`, application profiles and `from X to ... port N` rules;
// anything else is skipped rather than guessed.
func parseUFWShowAdded(output string) ufwSnapshot {
	snap := ufwSnapshot{Rules: map[string]string{}, Allows: map[string]string{}}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToLower(line), "ufw ") {
			continue
		}
		args := splitUFWArgs(strings.TrimSpace(line[len("ufw "):]))
		if len(args) < 2 {
			continue
		}
		originalArgs := append([]string(nil), args...)
		isRoute := strings.EqualFold(args[0], "route")
		action := strings.ToLower(args[0])
		if action == "route" {
			if len(args) < 3 {
				continue
			}
			action = strings.ToLower(args[1])
			args = args[1:]
		}
		if !isUFWStatusAction(action) {
			continue
		}
		target, comment, direction, source, hasDest := ufwShowAddedRule(args[1:])
		rule := ConfiguredRule{
			Target: target, Action: action, Direction: direction,
			Source: source, Comment: comment,
			Args: originalArgs,
		}
		// Route rules share destination keys with plain inbound rules but cover a
		// different flow; they stay replayable through Args and out of the
		// destination-keyed snapshot, which drives existed/prune decisions.
		if hasDest && !isRoute {
			snap.Rules[target] = comment
			if action == "allow" && direction != "out" {
				snap.Allows[target] = comment
			}
		}
		snap.configured = append(snap.configured, rule)
	}
	return snap
}

// ufwShowAddedRule returns the destination key, the comment and whether the
// rule is inbound ("in"/"") or outbound.
func ufwShowAddedRule(rest []string) (target, comment, direction, source string, hasDest bool) {
	comment = ""
	for i, token := range rest {
		if strings.EqualFold(token, "comment") {
			comment = strings.Trim(strings.Join(rest[i+1:], " "), `"'`)
			rest = rest[:i]
			break
		}
	}
	direction = "in"
	source = "Anywhere"
	for i, token := range rest {
		switch strings.ToLower(token) {
		case "out":
			direction = "out"
		case "from":
			if i+1 < len(rest) {
				source = rest[i+1]
			}
		}
	}
	for i := 0; i < len(rest); i++ {
		switch strings.ToLower(rest[i]) {
		case "app":
			if i+1 < len(rest) {
				return rest[i+1], comment, direction, source, true
			}
		case "port":
			if i+1 < len(rest) && isUFWPortToken(rest[i+1]) {
				spec := rest[i+1]
				if !strings.Contains(spec, "/") {
					proto := "tcp"
					if i+2 < len(rest) && strings.EqualFold(rest[i+2], "proto") && i+3 < len(rest) {
						proto = strings.ToLower(rest[i+3])
					}
					spec += "/" + proto
				}
				return spec, comment, direction, source, true
			}
		}
		if isUFWPortToken(rest[i]) {
			spec := rest[i]
			if !strings.Contains(spec, "/") {
				spec += "/tcp"
			}
			return spec, comment, direction, source, true
		}
	}
	// An application profile appears as a bare name (`ufw allow OpenSSH`). Only
	// before a `from`/`to` selector, otherwise the token is an address.
	sawSelector := false
	for _, token := range rest {
		lower := strings.ToLower(token)
		switch lower {
		case "from", "to":
			sawSelector = true
		case "in", "out", "on", "proto", "port", "app", "comment", "any":
			continue
		default:
			if !sawSelector && token != "" {
				return token, comment, direction, source, true
			}
		}
	}
	// No destination selector: `ufw allow from 1.2.3.4` applies to any
	// destination. It is kept for replay (Args) but must not enter the
	// destination-keyed snapshot, where it would look like a rule to 1.2.3.4.
	return "", comment, direction, source, false
}

func isUFWPortToken(token string) bool {
	if token == "" {
		return false
	}
	port := token
	if slash := strings.IndexByte(port, '/'); slash >= 0 {
		port = port[:slash]
	}
	start, end, hasRange := strings.Cut(port, ":")
	if !isUFWDigits(start) {
		return false
	}
	if hasRange && !isUFWDigits(end) {
		return false
	}
	return true
}

func isUFWDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// splitUFWArgs splits a `ufw show added` line on whitespace, keeping quoted
// segments (comments) together.
func splitUFWArgs(line string) []string {
	var args []string
	var current strings.Builder
	quote := rune(0)
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return args
}
