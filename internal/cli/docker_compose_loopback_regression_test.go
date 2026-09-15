package cli

import (
	"os"
	"strings"
	"testing"
)

func TestDockerComposeExampleUsesLoopbackServe(t *testing.T) {
	body, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	compose := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.Contains(compose, "--listen 0.0.0.0") {
		t.Fatal("compose example still binds a public listen that first-run exposure policy rejects")
	}
	if !strings.Contains(compose, "command: serve") {
		t.Fatal("compose example must use default loopback serve")
	}
}
