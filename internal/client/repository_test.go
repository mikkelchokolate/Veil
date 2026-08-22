package client

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestCreateAndGetClient(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c := Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever}
	created, err := repo.Create(c)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated ID")
	}
	if created.Version != 1 {
		t.Fatalf("expected version 1, got %d", created.Version)
	}

	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "alice" || got.ID != created.ID {
		t.Fatalf("unexpected client: %+v", got)
	}
}

func TestUpdateNameKeepsIDAndBumpsVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	c.Name = "alice-renamed"
	updated, err := repo.Update(c, c.Version)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != c.ID {
		t.Fatalf("ID must not change")
	}
	if updated.Name != "alice-renamed" {
		t.Fatalf("name not updated: %+v", updated)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestOptionalEmail(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "noemail", Enabled: true, QuotaResetPolicy: ResetNever})
	got, _ := repo.Get(c.ID)
	if got.Email != nil {
		t.Fatalf("expected nil email, got %v", *got.Email)
	}
	email := "a@b.c"
	c2, _ := repo.Create(Client{Name: "withemail", Enabled: true, QuotaResetPolicy: ResetNever, Email: &email})
	got2, _ := repo.Get(c2.ID)
	if got2.Email == nil || *got2.Email != email {
		t.Fatalf("expected email persisted, got %+v", got2.Email)
	}
}

func TestOptimisticLockingConflict(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	// First update succeeds (version 1 -> 2).
	c.Name = "v2"
	if _, err := repo.Update(c, 1); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// Second update with stale version 1 must conflict.
	c.Name = "v3"
	_, err := repo.Update(c, 1)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got %v", err)
	}
}

func TestBindingUniquePerClientInbound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b := Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true}
	if _, err := repo.CreateBinding(b); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	// Duplicate (client_id, inbound_id) must fail.
	if _, err := repo.CreateBinding(b); !errors.Is(err, ErrDuplicateBinding) {
		t.Fatalf("expected ErrDuplicateBinding, got %v", err)
	}
}

func TestOneClientMultipleBindings(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	for _, in := range []string{"hy2", "mieru", "naive"} {
		if _, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: in, Enabled: true}); err != nil {
			t.Fatalf("bind %s: %v", in, err)
		}
	}
	bindings, err := repo.BindingsForClient(c.ID)
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if len(bindings) != 3 {
		t.Fatalf("expected 3 bindings, got %d", len(bindings))
	}
}

func TestDeleteBindingKeepsClient(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy2", Enabled: true})
	if err := repo.DeleteBinding(b.ID); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	if _, err := repo.Get(c.ID); err != nil {
		t.Fatalf("client must survive binding delete: %v", err)
	}
}

func TestDeleteClientCascadeBindings(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "hy2", Enabled: true})
	if err := repo.Delete(c.ID); err != nil {
		t.Fatalf("delete client: %v", err)
	}
	if _, err := repo.GetBinding(b.ID); !errors.Is(err, sql.ErrNoRows) && err == nil {
		t.Fatalf("binding must be cascade-deleted")
	}
}

func TestOrphanClientDetection(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	orphan, _ := repo.Create(Client{Name: "orphan", Enabled: true, QuotaResetPolicy: ResetNever})
	attached, _ := repo.Create(Client{Name: "attached", Enabled: true, QuotaResetPolicy: ResetNever})
	_, _ = repo.CreateBinding(Binding{ClientID: attached.ID, InboundID: "hy2", Enabled: true})

	orphans, err := repo.OrphanClients()
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].ID != orphan.ID {
		t.Fatalf("expected 1 orphan (%s), got %+v", orphan.ID, orphans)
	}
}

func TestListPaginationAndSearch(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin"} {
		_, _ = repo.Create(Client{Name: name, Enabled: true, QuotaResetPolicy: ResetNever})
	}
	page, total, err := repo.List(ListFilter{Page: 1, PageSize: 2, Sort: "name"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 || len(page) != 2 {
		t.Fatalf("expected total=5 len=2, got total=%d len=%d", total, len(page))
	}
	found, total2, err := repo.List(ListFilter{Search: "carol"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total2 != 1 || found[0].Name != "carol" {
		t.Fatalf("search failed: %+v total=%d", found, total2)
	}
}

func TestListFilterExpiresBefore(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	soon := time.Now().Add(24 * time.Hour).Unix()
	later := time.Now().Add(90 * 24 * time.Hour).Unix()
	_, _ = repo.Create(Client{Name: "expiring", Enabled: true, QuotaResetPolicy: ResetNever, ExpiresAt: &soon})
	_, _ = repo.Create(Client{Name: "longlived", Enabled: true, QuotaResetPolicy: ResetNever, ExpiresAt: &later})

	cutoff := time.Now().Add(48 * time.Hour).Unix()
	items, total, err := repo.List(ListFilter{ExpiresBefore: &cutoff})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if total != 1 || items[0].Name != "expiring" {
		t.Fatalf("expiresBefore filter failed: %+v total=%d", items, total)
	}
}
