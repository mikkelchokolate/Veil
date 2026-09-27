package routing

import "testing"

// #1082: the #1071 comma semantics fold bare segments after a regexp atom
// into the regexp itself. A stored rule written under the old split therefore
// still parses — but means something different. MatchResplitsAtoms flags
// that silent semantic change so validators can warn.
func TestMatchResplitsAtoms(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		// regexp atom followed by a bare domain: pre-#1071 this was regex OR
		// suffix; now one regexp that can never match.
		{`regexp:\.ru$,example.org`, true},
		{`regexp:^a,b`, true},
		{`geosite:category-ru,regexp:\.ru$,example.org`, true},
		// Two regexp atoms still parse as separate atoms — prefix makes the
		// comma an atom separator, so nothing is folded.
		{`regexp:\.ru$,regexp:example.org`, false},
		{`geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`, false},
		// Plain comma splits with no regexp atom never fold.
		{"geoip:private,geosite:category-gov-ru", false},
		{"example.com,example.org", false},
		{"all", false},
		{`regexp:\.ru$`, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := MatchResplitsAtoms(tc.raw); got != tc.want {
			t.Errorf("MatchResplitsAtoms(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
