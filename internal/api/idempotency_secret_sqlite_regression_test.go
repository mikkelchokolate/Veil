package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneratedClientCredentialPlaintextNeverExistsInSQLite(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds", `{"name":"idem-secret-hy","protocol":"hysteria2","transport":"udp","port":30443,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", strings.NewReader(`{"name":"idem-secret-client","bindings":[{"inboundId":"idem-secret-hy","runtimeIdentity":"idem_secret_identity"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "create-secret-client")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Issued []struct {
			Plaintext string `json:"plaintext"`
		} `json:"issuedCredentials"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Issued) != 1 || body.Issued[0].Plaintext == "" {
		t.Fatalf("issued credentials response invalid: err=%v body=%s", err, response.Body.String())
	}
	plaintext := body.Issued[0].Plaintext

	rows, err := state.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		quotedTable := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		columnsRows, err := state.db.Query(`PRAGMA table_info(` + quotedTable + `)`)
		if err != nil {
			t.Fatal(err)
		}
		var columns []string
		for columnsRows.Next() {
			var cid, notNull, pk int
			var name, columnType string
			var defaultValue any
			if err := columnsRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
				columnsRows.Close()
				t.Fatal(err)
			}
			columns = append(columns, name)
		}
		if err := columnsRows.Close(); err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			quotedColumn := `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
			var found int
			query := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE instr(CAST(%s AS TEXT),?)>0)`, quotedTable, quotedColumn)
			if err := state.db.QueryRow(query, plaintext).Scan(&found); err != nil {
				t.Fatalf("scan %s.%s: %v", table, column, err)
			}
			if found != 0 {
				t.Fatalf("generated credential plaintext persisted in SQLite column %s.%s", table, column)
			}
		}
	}
}
