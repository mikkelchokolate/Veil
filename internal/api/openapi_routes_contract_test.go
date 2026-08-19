package api

import (
	"bufio"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEveryMutatingOpenAPIOperationDocumentsIdempotencyKey(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	currentPath := ""
	for index, line := range lines {
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			currentPath = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		method := strings.TrimSuffix(trimmed, ":")
		if indent != 4 || (method != "post" && method != "put" && method != "patch" && method != "delete") {
			continue
		}
		if (currentPath == "/api/auth/login" || currentPath == "/api/auth/logout") && method == "post" {
			continue
		}
		end := index + 1
		for end < len(lines) {
			next := lines[end]
			if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) <= 4 {
				break
			}
			end++
		}
		if !strings.Contains(strings.Join(lines[index:end], "\n"), "#/components/parameters/IdempotencyKey") {
			t.Errorf("%s %s does not document Idempotency-Key", strings.ToUpper(method), currentPath)
		}
	}
}

func TestOpenAPIRoutesAndMethodsMatchRegisteredAPI(t *testing.T) {
	actual := openAPIRouteMethods(t, "../../docs/openapi.yaml")
	expected := map[string][]string{
		"/healthz":                                     {"get"},
		"/metrics":                                     {"get"},
		"/api/setup/status":                            {"get"},
		"/api/setup/complete":                          {"post"},
		"/api/auth/login":                              {"post"},
		"/api/auth/logout":                             {"post"},
		"/api/auth/status":                             {"get"},
		"/api/auth/locale":                             {"post"},
		"/api/auth/sessions":                           {"delete", "get"},
		"/api/admin/rotate-key":                        {"post"},
		"/api/audit":                                   {"get"},
		"/api/backups":                                 {"get", "post"},
		"/api/backups/prune":                           {"post"},
		"/api/backups/{name}/download":                 {"get"},
		"/api/backups/{name}/verify":                   {"post"},
		"/api/backups/{name}":                          {"delete"},
		"/api/backups/{name}/restore":                  {"post"},
		"/api/backup-restore-jobs/{id}":                {"get"},
		"/api/users":                                   {"get", "post"},
		"/api/users/{username}":                        {"delete", "put"},
		"/api/status":                                  {"get"},
		"/api/version":                                 {"get"},
		"/api/version/update":                          {"post"},
		"/api/settings":                                {"get", "put"},
		"/api/protocols":                               {"get"},
		"/api/protocols/{protocol}/room":               {"post"},
		"/api/inbounds":                                {"get", "post"},
		"/api/inbounds/{name}":                         {"delete", "get", "put"},
		"/api/routing/rules":                           {"get", "post"},
		"/api/routing/rules/{name}":                    {"delete", "get", "put"},
		"/api/routing/presets":                         {"get"},
		"/api/routing/presets/{name}":                  {"post"},
		"/api/warp":                                    {"get", "put"},
		"/api/client-links":                            {"get"},
		"/api/client-links/subscription":               {"get"},
		"/api/client-links/qr":                         {"post"},
		"/api/firewall":                                {"get"},
		"/api/apply":                                   {"post"},
		"/api/apply/state":                             {"get"},
		"/api/apply/jobs":                              {"get"},
		"/api/apply/jobs/{id}":                         {"get"},
		"/api/apply/jobs/{id}/retry":                   {"post"},
		"/api/apply/reconcile":                         {"post"},
		"/api/apply/rollback":                          {"post"},
		"/api/validation":                              {"post"},
		"/api/apply/plan":                              {"post"},
		"/api/apply/history":                           {"get"},
		"/api/profiles/ru-recommended/preview":         {"post"},
		"/api/services/{name}/restart":                 {"post"},
		"/api/system":                                  {"get"},
		"/api/tls":                                     {"get"},
		"/api/network":                                 {"get"},
		"/api/connections":                             {"get"},
		"/api/processes":                               {"get"},
		"/api/disk":                                    {"get"},
		"/api/runtime/observation":                     {"get"},
		"/api/runtime/provenance":                      {"get"},
		"/api/logs":                                    {"get"},
		"/api/tools/dns-lookup":                        {"post"},
		"/api/tools/ping":                              {"post"},
		"/api/tools/speedtest":                         {"post"},
		"/api/v1/clients":                              {"get", "post"},
		"/api/v1/clients/bulk":                         {"post"},
		"/api/v1/clients/migrate-legacy":               {"post"},
		"/api/v1/clients/{id}":                         {"delete", "get", "patch"},
		"/api/v1/clients/{id}/bindings":                {"get", "post"},
		"/api/v1/clients/{id}/bindings/{bindingId}":    {"delete", "patch"},
		"/api/v1/clients/{id}/credentials/{bindingId}": {"post"},
		"/api/v1/clients/{id}/credentials/{bindingId}/rotate": {"post"},
		"/api/v1/clients/{id}/links":                          {"get"},
		"/api/v1/clients/{id}/tokens":                         {"get", "post"},
		"/api/v1/clients/{id}/tokens/{tokenId}":               {"delete", "get"},
		"/api/v1/clients/{id}/tokens/{tokenId}/rotate":        {"post"},
		"/api/v1/traffic/summary":                             {"get"},
		"/api/v1/traffic/top":                                 {"get"},
		"/api/v1/traffic/{id}":                                {"get"},
		"/api/v1/traffic/{id}/history":                        {"get"},
		"/api/v1/traffic/stream":                              {"get"},
		"/api/v1/events":                                      {"get"},
		"/s/{token}":                                          {"get", "head"},
	}
	for path := range expected {
		sort.Strings(expected[path])
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("OpenAPI route contract drift:\nactual=%v\nexpected=%v", actual, expected)
	}
}

func openAPIRouteMethods(t *testing.T, path string) map[string][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result := map[string][]string{}
	currentPath := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":") {
			currentPath = strings.TrimSuffix(strings.TrimSpace(line), ":")
			result[currentPath] = nil
			continue
		}
		if currentPath == "" || len(line) < 5 || line[:4] != "    " || line[4] == ' ' {
			continue
		}
		method := strings.TrimSuffix(strings.TrimSpace(line), ":")
		switch method {
		case "get", "post", "put", "patch", "delete", "head", "options", "trace":
			result[currentPath] = append(result[currentPath], method)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for path := range result {
		sort.Strings(result[path])
	}
	return result
}
