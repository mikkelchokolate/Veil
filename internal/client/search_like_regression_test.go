package client

import (
	"testing"
)

func TestListSearchTreatsUnderscoreAndPercentAsLiterals(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	email := "alpha_01@example.test"
	for _, c := range []Client{
		{Name: "alpha_01", Email: &email, Enabled: true, QuotaResetPolicy: ResetNever},
		{Name: "alphab01", Enabled: true, QuotaResetPolicy: ResetNever},
		{Name: "ordinary", Enabled: true, QuotaResetPolicy: ResetNever},
		{Name: "100%ready", Enabled: true, QuotaResetPolicy: ResetNever},
		{Name: "100xready", Enabled: true, QuotaResetPolicy: ResetNever},
		{Name: "esc!ape", Enabled: true, QuotaResetPolicy: ResetNever},
	} {
		if _, err := repo.Create(c); err != nil {
			t.Fatalf("create %s: %v", c.Name, err)
		}
	}

	mustNames := func(t *testing.T, search string, want ...string) {
		t.Helper()
		items, total, err := repo.List(ListFilter{Search: search, Page: 1, PageSize: 50, Sort: "name"})
		if err != nil {
			t.Fatalf("search %q: %v", search, err)
		}
		if total != len(want) {
			t.Fatalf("search %q total=%d want %d items=%v", search, total, len(want), namesOf(items))
		}
		got := namesOf(items)
		if len(got) != len(want) {
			t.Fatalf("search %q got %v want %v", search, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("search %q got %v want %v", search, got, want)
			}
		}
	}

	mustNames(t, "alpha_01", "alpha_01")
	mustNames(t, "%", "100%ready")
	mustNames(t, "100%", "100%ready")
	mustNames(t, "!", "esc!ape")
	mustNames(t, "ordinary", "ordinary")
	items, total, err := repo.List(ListFilter{Search: "", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 || len(items) != 6 {
		t.Fatalf("empty search total=%d len=%d", total, len(items))
	}
}

func namesOf(items []Client) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
