package api

import (
	"fmt"
	"sort"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

// Unit tests replace this network/machine boundary, like the service runner.
var provisionApplyRuntimes = installApplyRuntimes

func installApplyRuntimes(ctx ManagementApplyContext) error {
	names, err := requiredRuntimeNames(ctx.state.settings, ctx.state.inbounds, ctx.state.warp)
	if err != nil || len(names) == 0 {
		return err
	}
	installer, ok := ctx.state.privileged.(privileged.RuntimeInstaller)
	if !ok {
		return fmt.Errorf("automatic runtime installation requires an updated privileged helper")
	}
	stopObserving := ctx.observeRuntimeProgress()
	defer stopObserving()
	if err := installer.InstallRuntimes(ctx.operationContext(), privileged.RuntimeInstallRequest{
		Names: names, Fence: ctx.fenceToken(),
	}); err != nil {
		return fmt.Errorf("install required runtimes: %w", err)
	}
	return nil
}

func requiredRuntimeNames(settings Settings, inbounds []Inbound, warp WarpConfig) ([]string, error) {
	selected := map[string]bool{}
	registry := protocols.NewRegistry()
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		plugin, ok := registry.Get(inbound.Protocol)
		if !ok {
			return nil, fmt.Errorf("unknown inbound protocol %q", inbound.Protocol)
		}
		if runtime, ok := protocols.AsRuntimeProvider(plugin); ok {
			selected[runtime.RuntimeInstall("").Name] = true
		}
	}
	if caddyRequired(settings, inbounds) {
		selected["naiveproxy"] = true
	}
	if warp.Enabled {
		selected["warp"] = true
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
