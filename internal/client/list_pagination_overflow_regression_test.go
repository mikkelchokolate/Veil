package client

import (
	"math"
	"testing"
)

// TestListHugePageDoesNotWrapToFirstPage pins #1272 item 5: (page-1)*size
// used to overflow int for a huge page, and SQLite clamps a negative OFFSET
// to 0, so a request far past the end silently returned page 1. A page past
// the end must be empty while total still reports every match.
func TestListHugePageDoesNotWrapToFirstPage(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	for _, name := range []string{"page-a", "page-b", "page-c"} {
		if _, err := repo.Create(Client{Name: name, Enabled: true, QuotaResetPolicy: ResetNever}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	for _, tc := range []struct {
		name       string
		page, size int
	}{
		// Overflow arithmetic below is spelled out for 64-bit int; on 32-bit
		// the same cases overflow too and the assertions are unchanged.
		// 64-bit: (MaxInt/2+2-1)*2 = 2^63 wraps to math.MinInt64.
		{"wraps-negative", math.MaxInt/2 + 2, 2},
		// (MaxInt-1)*500 overflows and wraps.
		{"max-page", math.MaxInt, 500},
		// (MaxInt/2+1)*4 = 2^(bits) wraps to exactly 0 — the first page.
		{"wraps-to-zero", math.MaxInt/2 + 2, 4},
		// Plain past-the-end page with no overflow at all.
		{"past-end", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := repo.List(ListFilter{Page: tc.page, PageSize: tc.size, Sort: "name"})
			if err != nil {
				t.Fatalf("list page=%d size=%d: %v", tc.page, tc.size, err)
			}
			if total != 3 {
				t.Errorf("total=%d, want 3", total)
			}
			if len(items) != 0 {
				names := make([]string, 0, len(items))
				for _, c := range items {
					names = append(names, c.Name)
				}
				t.Fatalf("page=%d size=%d returned %v, want an empty page past the end", tc.page, tc.size, names)
			}
		})
	}

	// Control: an ordinary page still works.
	items, _, err := repo.List(ListFilter{Page: 2, PageSize: 2, Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "page-c" {
		t.Fatalf("page 2 size 2 = %+v, want [page-c]", items)
	}
}
