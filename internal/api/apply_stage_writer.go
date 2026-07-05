package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

var routeDatDownloader = generatedconfig.DownloadRouteDat

type ApplyStageInput struct {
	ApplyRoot     string
	Cipher        *secrets.Cipher
	Plan          ApplyPlanResponse
	Snapshot      managementSnapshot
	Rendered      map[string]string
	RoutingSource RoutingSource
	Validate      func([]string) []ConfigValidationResult
}

func WriteApplyStage(input ApplyStageInput) ([]string, []ConfigValidationResult, []string, error) {
	stageDir := filepath.Join(input.ApplyRoot, "generated", "veil")
	planPath := filepath.Join(stageDir, "apply-plan.json")
	statePath := filepath.Join(stageDir, "management-state.json")
	planBody, err := json.MarshalIndent(input.Plan, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := atomicfile.Write(planPath, append(planBody, '\n'), 0o600, 0o700); err != nil {
		return nil, nil, nil, err
	}
	snapshotBody, err := managementstate.NewStore("", input.Cipher).Marshal(input.Snapshot)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := atomicfile.Write(statePath, snapshotBody, 0o600, 0o700); err != nil {
		return nil, nil, nil, err
	}
	written := []string{planPath, statePath}
	renderedPaths := sortedRenderedPaths(input.Rendered)
	for _, path := range renderedPaths {
		if err := atomicfile.Write(path, []byte(input.Rendered[path]), 0o600, 0o700); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, path)
	}
	routingFiles, err := generatedconfig.NewRoutingSourceMaterial(input.ApplyRoot, input.RoutingSource).WithDownloader(routeDatDownloader).WriteGenerated()
	if err != nil {
		return nil, nil, nil, err
	}
	written = append(written, routingFiles...)

	// Remove generated config files from previous applies that are no longer
	// part of the desired staged set. This ensures deleted inbounds (or disabled
	// features such as WARP) do not leave stale artifacts behind.
	keep := make(map[string]bool, len(written))
	for _, p := range written {
		keep[p] = true
	}
	if err := cleanStaleGeneratedFiles(filepath.Join(input.ApplyRoot, "generated"), keep); err != nil {
		return nil, nil, nil, err
	}

	validate := input.Validate
	if validate == nil {
		validate = stagedConfigValidator
	}
	validations := validate(renderedPaths)
	return written, validations, renderedPaths, nil
}

var managedGeneratedSubdirs = []string{"caddy", "hysteria2", "mieru", "olcrtc", "sing-box", "rules"}

func cleanStaleGeneratedFiles(generatedRoot string, keep map[string]bool) error {
	for _, subdir := range managedGeneratedSubdirs {
		root := filepath.Join(generatedRoot, subdir)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() || !info.Mode().IsRegular() {
				return nil
			}
			if keep[path] {
				return nil
			}
			return os.Remove(path)
		}); err != nil {
			return err
		}
	}
	return nil
}

func sortedRenderedPaths(rendered map[string]string) []string {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
