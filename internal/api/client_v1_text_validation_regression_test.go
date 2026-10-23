package api

import (
	"net/http"
	"strconv"
	"testing"
)

// #1272 item 7 at the HTTP boundary: out-of-policy email/groupId/notes are a
// 400 on create and PATCH, while a PATCH that omits a legacy value stored by
// an earlier build still succeeds and keeps it.
func TestV1ClientTextFieldsValidatedAndLegacyValuesGrandfathered(t *testing.T) {
	r, state := newApplyTrackedRouterWithState(t)

	for name, body := range map[string]string{
		"email control": `{"name":"bad-email","email":"a@b\nBcc: x"}`,
		"group control": `{"name":"bad-group","groupId":"g\u0000"}`,
		"notes bidi":    `{"name":"bad-notes","notes":"a\u202eb"}`,
	} {
		if res := v1Request(t, r, http.MethodPost, "/api/v1/clients", body); res.Code != http.StatusBadRequest {
			t.Errorf("create with %s: status=%d body=%s, want 400", name, res.Code, res.Body.String())
		}
	}

	created := createFullyPopulatedClient(t, r, "text-legacy")
	id := created["id"].(string)
	version := int(created["version"].(float64))
	legacyNotes := "legacy \x1b[31m notes"
	legacyEmail := "legacy\x01@example.test"
	legacyGroup := "legacy\x7fgroup"
	// Raw write stands in for a row persisted before the limits existed.
	if _, err := state.db.Exec(`UPDATE clients SET notes=?, email=?, group_id=? WHERE id=?`,
		legacyNotes, legacyEmail, legacyGroup, id); err != nil {
		t.Fatal(err)
	}

	res := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id,
		`{"version":`+strconv.Itoa(version)+`,"enabled":false,"name":"still-editable"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("PATCH leaving legacy text untouched: status=%d body=%s", res.Code, res.Body.String())
	}
	updated := decodeJSONMap(t, res.Body.Bytes())
	if updated["notes"] != legacyNotes || updated["email"] != legacyEmail || updated["groupId"] != legacyGroup {
		t.Fatalf("legacy text not preserved: notes=%q email=%q groupId=%q",
			updated["notes"], updated["email"], updated["groupId"])
	}
	version = int(updated["version"].(float64))

	for name, patch := range map[string]string{
		"notes":   `"notes":"new \u0000 notes"`,
		"email":   `"email":"other\u0002@example.test"`,
		"groupId": `"groupId":"other\u0003group"`,
	} {
		res := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, `{"version":`+strconv.Itoa(version)+`,`+patch+`}`)
		if res.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s to a new invalid value: status=%d body=%s, want 400", name, res.Code, res.Body.String())
		}
	}

	res = v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id,
		`{"version":`+strconv.Itoa(version)+`,"notes":"fixed\nnotes","email":"fixed@example.test","groupId":"fixed-group"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("PATCH fixing legacy text: status=%d body=%s", res.Code, res.Body.String())
	}
}
