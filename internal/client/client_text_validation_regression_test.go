package client

import (
	"errors"
	"strings"
	"testing"
)

// #1272 item 7: email, groupId and notes used to be accepted verbatim
// (control characters, invalid UTF-8, unbounded size). They now follow the
// same boundary rules as the display name, with multi-line notes allowed.

func TestClientTextValidationOnCreate(t *testing.T) {
	str := func(s string) *string { return &s }
	valid := []Client{
		{Email: str("alice@example.com")},
		{Email: str("")},
		{Email: str(strings.Repeat("a", MaxClientEmailBytes))},
		{GroupID: str("production-group")},
		{GroupID: str("Группа 山田")},
		{GroupID: str(strings.Repeat("界", MaxClientGroupIDRunes))},
		{Notes: "line one\nline two\r\n\tindented"},
		{Notes: "family 👨\u200d👩\u200d👧 soft\u00adhyphen"},
		{Notes: strings.Repeat("界", MaxClientNotesRunes)},
	}
	for i, c := range valid {
		c.Name, c.QuotaResetPolicy = "x", ResetNever
		if err := validate(c); err != nil {
			t.Errorf("valid case %d (%+v): %v", i, c, err)
		}
	}
	bad := string([]byte{0xff, 0xfe})
	invalid := map[string]Client{
		"email too long":          {Email: str(strings.Repeat("a", MaxClientEmailBytes+1))},
		"email control":           {Email: str("a@b\nBcc: x")},
		"email surrounding space": {Email: str(" a@b.c")},
		"email bidi":              {Email: str("a\u202eb@c")},
		"email invalid utf8":      {Email: str(bad)},
		"group too long":          {GroupID: str(strings.Repeat("界", MaxClientGroupIDRunes+1))},
		"group control":           {GroupID: str("g\x00")},
		"group zero width":        {GroupID: str("g\u200bh")},
		"group surrounding space": {GroupID: str("g ")},
		"notes too long":          {Notes: strings.Repeat("a", MaxClientNotesRunes+1)},
		"notes NUL":               {Notes: "a\x00b"},
		"notes ESC":               {Notes: "a\x1b[31mred"},
		"notes C1":                {Notes: "a\u0085b"},
		"notes RLO":               {Notes: "a\u202eb"},
		"notes isolate":           {Notes: "a\u2066b"},
		"notes RLM":               {Notes: "a\u200fb"},
		"notes ALM":               {Notes: "a\u061cb"},
		"notes invalid utf8":      {Notes: bad},
	}
	for name, c := range invalid {
		c.Name, c.QuotaResetPolicy = "x", ResetNever
		if err := validate(c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", name, err)
		}
	}
}

// A row stored by a build that predates the limits must stay editable: an
// update that leaves the legacy value untouched passes, while replacing it
// with another out-of-policy value is refused.
func TestClientTextValidationGrandfathersUnchangedLegacyValues(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, NewCredentialStore(db, newTestCipher(t)))

	legacyEmail := "legacy\x01@example.com"
	legacyGroup := "legacy\tgroup"
	legacyNotes := "legacy \u202e notes" + strings.Repeat("n", MaxClientNotesRunes)
	// Repository.Create is the raw writer: it stands in for a row persisted
	// before validate() covered these fields.
	stored, err := repo.Create(Client{
		Name: "legacy", Email: &legacyEmail, GroupID: &legacyGroup, Notes: legacyNotes,
		Enabled: true, QuotaResetPolicy: ResetNever,
	})
	if err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	toggled := stored
	toggled.Enabled = false
	toggled.Name = "renamed"
	if err := repo.WithTx(func(tx *Tx) error { return svc.UpdateTx(tx, toggled, stored.Version) }); err != nil {
		t.Fatalf("update leaving legacy text untouched: %v", err)
	}
	current, err := repo.Get(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Enabled || current.Name != "renamed" || current.Notes != legacyNotes ||
		*current.Email != legacyEmail || *current.GroupID != legacyGroup {
		t.Fatalf("unexpected row after update: %+v", current)
	}

	otherBad := "still\x02bad@example.com"
	changes := map[string]func(*Client){
		"email": func(c *Client) { c.Email = &otherBad },
		"group": func(c *Client) { g := "g\x00"; c.GroupID = &g },
		"notes": func(c *Client) { c.Notes = "new \x1b notes" },
	}
	for name, mutate := range changes {
		next := current
		mutate(&next)
		err := repo.WithTx(func(tx *Tx) error { return svc.UpdateTx(tx, next, current.Version) })
		if !errors.Is(err, ErrValidation) {
			t.Errorf("UpdateTx changing %s to an invalid value: got %v, want ErrValidation", name, err)
		}
		if _, err := svc.Update(next, current.Version); !errors.Is(err, ErrValidation) {
			t.Errorf("Update changing %s to an invalid value: got %v, want ErrValidation", name, err)
		}
	}

	// Clearing or fixing the legacy value is always allowed.
	fixed := current
	fixed.Email, fixed.GroupID, fixed.Notes = nil, nil, ""
	if err := repo.WithTx(func(tx *Tx) error { return svc.UpdateTx(tx, fixed, current.Version) }); err != nil {
		t.Fatalf("clearing legacy text: %v", err)
	}
}

// Rollback restores revision snapshots that may predate the limits; only the
// runtime-relevant rules (validateCore) veto a restore.
func TestValidateRestoredClientIgnoresFreeTextLimits(t *testing.T) {
	email := "legacy\x01@example.com"
	c := Client{Name: "x", QuotaResetPolicy: ResetNever, Email: &email, Notes: "a\x00b"}
	if err := ValidateRestoredClient(c); err != nil {
		t.Fatalf("restored legacy free text: %v", err)
	}
	c.Name = " bad"
	if err := ValidateRestoredClient(c); !errors.Is(err, ErrValidation) {
		t.Fatalf("restored malformed name: got %v, want ErrValidation", err)
	}
}
