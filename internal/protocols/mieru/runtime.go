package mieru

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// RuntimeDescriptors returns the single aggregated mieru unit.
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	// Always expose the aggregated unit so capability metadata and empty-state
	// runtime catalogs can discover it. Mieru configs aggregate all enabled
	// inbounds into a single server_config.json, so there is no per-inbound
	// branch.
	_ = enabledInbounds
	return []service.ManagedRuntime{{
		Name:             "mieru",
		ActionName:       "mieru",
		Protocol:         p.Protocol(),
		Unit:             "veil-mieru.service",
		PromotedSubpath:  generatedconfig.MieruConfigSubpath,
		PromotedVerb:     "restart",
		ManualRestart:    true,
		HealthCheckAfter: true,
	}}
}

// RuntimeInstall returns the Mieru runtime descriptor.
func (Plugin) RuntimeInstall(arch string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{
		Name:           "mieru",
		Binary:         "mita",
		Method:         runtimeinstall.MethodArchive,
		Repo:           "enfein/mieru",
		Version:        "v3.34.1",
		Integrity:      "upstream-checksum",
		VersionArgs:    []string{"version"},
		VersionCommand: "mita version",
		VersionPattern: `(?i)3\.34\.1`,
		Description:    "mita is downloaded from its upstream GitHub release",
		AssetMatch: func(name string) bool {
			return startsWith(name, "mita_") && endsWith(name, "_linux_"+arch+".tar.gz")
		},
		ChecksumMatch: func(name string) bool {
			return startsWith(name, "mita_") && endsWith(name, "_linux_"+arch+".tar.gz.sha256.txt")
		},
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
