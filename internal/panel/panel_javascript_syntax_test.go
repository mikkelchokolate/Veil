package panel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderedPanelJavaScriptParsesWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; skipping rendered JavaScript syntax gate")
	}

	panelHTML := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	pages := map[string]string{
		"panel": AuthenticationExpiryReliableHTML(StorageReliableHTML(panelHTML)),
		"login": ReliableLoginHTML("/", LocaleEnglish),
		"setup": ReliableSetupHTML("/", LocaleEnglish),
	}

	for name, html := range pages {
		name, html := name, html
		t.Run(name, func(t *testing.T) {
			scripts := renderedInlineScripts(html)
			if len(scripts) == 0 {
				t.Fatalf("rendered %s page contains no inline scripts", name)
			}
			for index, script := range scripts {
				path := filepath.Join(t.TempDir(), fmt.Sprintf("%s-%d.js", name, index))
				if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
					t.Fatalf("write rendered %s script: %v", name, err)
				}
				command := exec.Command(node, "--check", path)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("rendered %s JavaScript block %d does not parse: %v\n%s", name, index, err, output)
				}
			}
		})
	}
}

func renderedInlineScripts(html string) []string {
	var scripts []string
	remaining := html
	for {
		start := strings.Index(remaining, "<script")
		if start < 0 {
			return scripts
		}
		remaining = remaining[start:]
		openingEnd := strings.Index(remaining, ">")
		if openingEnd < 0 {
			return scripts
		}
		openingTag := remaining[:openingEnd+1]
		remaining = remaining[openingEnd+1:]
		closing := strings.Index(remaining, "</script>")
		if closing < 0 {
			return scripts
		}
		if !strings.Contains(strings.ToLower(openingTag), " src=") {
			scripts = append(scripts, remaining[:closing])
		}
		remaining = remaining[closing+len("</script>"):]
	}
}
