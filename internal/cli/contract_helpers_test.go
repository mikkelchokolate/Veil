package cli

// Shared helpers for workflow/shell/packaging contract tests. Every helper
// here exists to defeat the same false-green shape: an assertion that a
// comment or an unrelated job/step can satisfy.

import (
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// stripHashComments removes `#` comments from shell/YAML text so a substring
// assertion cannot be satisfied by comment prose. A `#` opens a comment only
// at line start (after whitespace) or when preceded by whitespace outside
// single/double quotes — matching POSIX shell and YAML comment rules.
func stripHashComments(t *testing.T, text string) string {
	t.Helper()
	var b strings.Builder
	b.Grow(len(text))
	inSingle, inDouble := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			b.WriteByte(c)
			continue
		}
		if inDouble {
			if c == '"' {
				inDouble = false
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\'':
			inSingle = true
			b.WriteByte(c)
		case '"':
			inDouble = true
			b.WriteByte(c)
		case '#':
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' || text[i-1] == '\n' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				if i < len(text) {
					b.WriteByte('\n')
				}
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// stripGoComments parses a Go source file and reprints it without comments,
// so source-level substring assertions cannot be satisfied by doc comments.
func stripGoComments(t *testing.T, path string) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var b strings.Builder
	if err := printer.Fprint(&b, fset, file); err != nil {
		t.Fatalf("reprint %s: %v", path, err)
	}
	return b.String()
}

// workflowJobBlock returns the YAML block of a single job — the `  <name>:`
// key line at two-space indent through the next key at the same or shallower
// indent. Markers found inside the block provably belong to that job, not a
// sibling job or a shared comment.
func workflowJobBlock(t *testing.T, workflow, job string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	want := "  " + job + ":"
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == want {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("workflow lacks a literal %q job key", want)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "   ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// workflowJobNeeds parses the workflow with yaml.v3 and returns the declared
// needs list of one job (scalar and sequence forms both handled).
func workflowJobNeeds(t *testing.T, workflow, job string) []string {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			Needs yaml.Node `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflow), &doc); err != nil {
		t.Fatalf("workflow is not valid YAML: %v", err)
	}
	entry, ok := doc.Jobs[job]
	if !ok {
		t.Fatalf("workflow lacks job %q", job)
	}
	var needs []string
	switch entry.Needs.Kind {
	case yaml.ScalarNode:
		needs = append(needs, entry.Needs.Value)
	case yaml.SequenceNode:
		for _, node := range entry.Needs.Content {
			needs = append(needs, node.Value)
		}
	}
	return needs
}

// makefileRecipe returns the tab-indented recipe lines under `target:` —
// `.PHONY` declarations and comment blocks cannot satisfy a recipe assert.
func makefileRecipe(t *testing.T, makefile, target string) []string {
	t.Helper()
	lines := strings.Split(makefile, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, target+":") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("Makefile lacks target %q", target)
	}
	var recipe []string
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "\t") {
			recipe = append(recipe, strings.TrimSpace(line))
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		break
	}
	return recipe
}

// dockerfileInstructions returns logical Dockerfile instructions: comment
// lines dropped, continuation lines joined. Substring asserts on instructions
// cannot be satisfied by Dockerfile comments.
func dockerfileInstructions(t *testing.T, dockerfile string) []string {
	t.Helper()
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, line := range strings.Split(dockerfile, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasSuffix(trimmed, "\\") {
			cur.WriteString(strings.TrimSuffix(trimmed, "\\"))
			cur.WriteByte(' ')
			continue
		}
		cur.WriteString(trimmed)
		flush()
	}
	flush()
	return out
}
