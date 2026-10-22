package client

import (
	"database/sql"
	"errors"
	"testing"
)

type countedCredentialQueries struct {
	*sql.DB
	queries int
}

func (q *countedCredentialQueries) Query(query string, args ...any) (*sql.Rows, error) {
	q.queries++
	return q.DB.Query(query, args...)
}

func TestActiveCredentialUsesOneQuery(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	c, err := repo.Create(Client{Name: "one-query", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	query := &countedCredentialQueries{DB: db}
	if _, err := activeCredentialQ(query, binding.ID, "password"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing credential error = %v", err)
	}
	for _, plaintext := range []string{"first", "rotated"} {
		want, err := store.Set(binding.ID, "password", plaintext)
		if err != nil {
			t.Fatal(err)
		}
		query.queries = 0
		got, err := activeCredentialQ(query, binding.ID, "password")
		if err != nil || got.ID != want.ID || string(got.EncryptedValue) != string(want.EncryptedValue) {
			t.Fatalf("active credential mismatch: %v", err)
		}
		if query.queries != 1 {
			t.Fatalf("active credential used %d queries, want 1", query.queries)
		}
	}
}

func TestClientViewReusesActiveCredentialForPresence(t *testing.T) {
	svc := &Service{now: func() int64 { return 100 }}
	lists := 0
	view, err := svc.viewWith(Client{ID: "client", Enabled: true},
		func(string) ([]Binding, error) { return []Binding{{ID: "binding"}}, nil },
		func(string, string) (Credential, error) { return Credential{ID: "active", Kind: "password"}, nil },
		func(string) ([]Credential, error) { lists++; return nil, nil },
	)
	if err != nil || !view.HasCreds || lists != 0 {
		t.Fatalf("active credential presence: has=%v lists=%d err=%v", view.HasCreds, lists, err)
	}
	view, err = svc.viewWith(Client{ID: "client", Enabled: true},
		func(string) ([]Binding, error) { return []Binding{{ID: "binding"}}, nil },
		func(string, string) (Credential, error) { return Credential{}, sql.ErrNoRows },
		func(string) ([]Credential, error) { lists++; return []Credential{{ID: "revoked"}}, nil },
	)
	if err != nil || !view.HasCreds || lists != 1 || view.Bindings[0].Credential != nil {
		t.Fatalf("historical credential presence: has=%v lists=%d err=%v", view.HasCreds, lists, err)
	}
}
