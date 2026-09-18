package hysteria2

import (
	"os"

	"gopkg.in/yaml.v3"
)

// RenderedTrafficStats reads the trafficStats block of a rendered Hysteria2
// server config — the endpoint and credential the running unit actually
// serves. The management traffic provider uses it so telemetry authenticates
// against the published runtime artifact: a partially failed apply can leave
// the unit running a newer rendered config than the last applied snapshot,
// which makes any snapshot-derived credential permanently stale (HTTP 401).
// It reports ok=false when the file or the block is absent so the caller can
// fall back to the snapshot-derived credential.
func RenderedTrafficStats(path string) (listen, secret string, ok bool, err error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	var doc struct {
		TrafficStats *struct {
			Listen string `yaml:"listen"`
			Secret string `yaml:"secret"`
		} `yaml:"trafficStats"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return "", "", false, err
	}
	if doc.TrafficStats == nil || doc.TrafficStats.Listen == "" || doc.TrafficStats.Secret == "" {
		return "", "", false, nil
	}
	return doc.TrafficStats.Listen, doc.TrafficStats.Secret, true, nil
}
