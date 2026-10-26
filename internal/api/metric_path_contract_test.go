package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/observability"
)

// TestMetricPathLabelsCoverEveryRegisteredEndpoint locks #1443: the
// observability route table drifted from the authorization table, so live
// routes were collapsed into /{unmatched} or attributed to a sibling template
// (traffic/history counted as per-client traffic, backups/sftp as an archive
// operation, 2FA and passkey endpoints invisible). endpointPolicies is the
// method+template source of truth, so every registered template must normalize
// to itself.
func TestMetricPathLabelsCoverEveryRegisteredEndpoint(t *testing.T) {
	for _, policy := range endpointPolicies {
		if got := observability.NormalizeHTTPPath(policy.pattern); got != policy.pattern {
			t.Errorf("%s %s is labelled %q; add it to metricPathPatterns", policy.method, policy.pattern, got)
		}
	}
}
