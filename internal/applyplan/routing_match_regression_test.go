package applyplan

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// #1082: stored/restored/rolled-back routing rules never go through
// RoutingRuleValidation, so a match the stricter ParseMatch rejects must be a
// planner error — otherwise every renderer silently drops the whole rule and
// the plan still reports zero issues.
func TestBuildErrorsOnUnparseableStoredRoutingMatch(t *testing.T) {
	for _, match := range []string{
		"geosite:category-ru,example.com:443", // ':'-bearing bare atom — the #1071 rejection
		"geoip:ru,gip:cn",                     // typo'd kind
		"example.com:443",
	} {
		plan := Build(Input{
			Rules: []model.RoutingRule{{Name: "r1", Match: match, Outbound: "direct", Enabled: true}},
		})
		if plan.Valid {
			t.Fatalf("match %q: plan must not be valid for an unparseable stored rule", match)
		}
		found := false
		for _, e := range plan.Errors {
			if strings.Contains(e, "r1") && strings.Contains(e, "invalid match") {
				found = true
			}
		}
		if !found {
			t.Fatalf("match %q: expected a plan error naming the rule, got %v", match, plan.Errors)
		}
	}
}

// A disabled stored rule still renders nothing, so it must not block the
// plan — but an enabled one that merely re-splits warns instead of failing.
func TestBuildSkipsMatchValidationForDisabledRules(t *testing.T) {
	plan := Build(Input{
		Rules: []model.RoutingRule{{Name: "r1", Match: "example.com:443", Outbound: "direct", Enabled: false}},
	})
	if !plan.Valid {
		t.Fatalf("disabled rule must not invalidate the plan, errors: %v", plan.Errors)
	}
}

// A stored rule that still parses but re-splits differently than a plain
// comma split silently changed meaning — warn loudly without blocking apply.
func TestBuildWarnsOnResplitRoutingMatch(t *testing.T) {
	plan := Build(Input{
		Rules: []model.RoutingRule{{Name: "r1", Match: `regexp:\.ru$,example.org`, Outbound: "direct", Enabled: true}},
	})
	if !plan.Valid {
		t.Fatalf("resplit match still parses — plan should stay valid, errors: %v", plan.Errors)
	}
	found := false
	for _, issue := range plan.Issues {
		if issue.Code != "routing_match_resplit" {
			continue
		}
		found = true
		if issue.Severity != "warning" {
			t.Fatalf("issue severity = %q, want warning", issue.Severity)
		}
		if !strings.Contains(issue.Message, "r1") {
			t.Fatalf("issue should name the rule: %q", issue.Message)
		}
	}
	if !found {
		t.Fatalf("expected routing_match_resplit issue, got %v", plan.Issues)
	}
}

// Two prefixed regexp atoms are not a resplit: the comma between them is an
// atom separator, so no warning is emitted for the intended grammar.
func TestBuildNoResplitWarningForSeparateRegexpAtoms(t *testing.T) {
	plan := Build(Input{
		Rules: []model.RoutingRule{{Name: "r1", Match: `regexp:\.ru$,regexp:example.org`, Outbound: "direct", Enabled: true}},
	})
	for _, issue := range plan.Issues {
		if issue.Code == "routing_match_resplit" {
			t.Fatalf("unexpected resplit warning for separate prefixed atoms: %v", issue)
		}
	}
}
