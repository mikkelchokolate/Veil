package runtimeinstall

import (
	"fmt"
	"sort"
	"strings"
)

// NormalizeRuntimeNames lowercases, trims, and de-duplicates requested names
// while preserving first-seen order.
func NormalizeRuntimeNames(only []string) []string {
	seen := make(map[string]struct{}, len(only))
	out := make([]string, 0, len(only))
	for _, name := range only {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// FilterCatalog returns the runtimes named by only. An empty only list means
// every catalog entry. Unknown names are an error and produce no selection, so
// callers must not install a partial match.
func FilterCatalog(runtimes []Runtime, only []string) ([]Runtime, error) {
	names := NormalizeRuntimeNames(only)
	if len(names) == 0 {
		return append([]Runtime(nil), runtimes...), nil
	}

	byName := make(map[string]Runtime, len(runtimes))
	supportedSet := make(map[string]struct{}, len(runtimes))
	supported := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		key := strings.ToLower(strings.TrimSpace(runtime.Name))
		if key == "" {
			continue
		}
		byName[key] = runtime
		if _, ok := supportedSet[key]; ok {
			continue
		}
		supportedSet[key] = struct{}{}
		supported = append(supported, runtime.Name)
	}
	sort.Strings(supported)

	unknown := make([]string, 0)
	filtered := make([]Runtime, 0, len(names))
	for _, name := range names {
		runtime, ok := byName[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		filtered = append(filtered, runtime)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown runtime name(s) %s; supported: %s", strings.Join(unknown, ", "), strings.Join(supported, ", "))
	}
	return filtered, nil
}
