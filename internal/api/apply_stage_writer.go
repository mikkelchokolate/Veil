package api

import (
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/veil-panel/veil/internal/managementstate"
	"github.com/veil-panel/veil/internal/secrets"
)

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
	if err := writeAtomicFile(planPath, append(planBody, '\n'), 0o600); err != nil {
		return nil, nil, nil, err
	}
	snapshotBody, err := managementstate.NewStore("", input.Cipher).Marshal(input.Snapshot)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := writeAtomicFile(statePath, snapshotBody, 0o600); err != nil {
		return nil, nil, nil, err
	}
	written := []string{planPath, statePath}
	renderedPaths := sortedRenderedPaths(input.Rendered)
	for _, path := range renderedPaths {
		if err := writeAtomicFile(path, []byte(input.Rendered[path]), 0o600); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, path)
	}
	routingFiles, err := NewRoutingSourceMaterial(input.ApplyRoot, input.RoutingSource).WriteGenerated()
	if err != nil {
		return nil, nil, nil, err
	}
	written = append(written, routingFiles...)
	validate := input.Validate
	if validate == nil {
		validate = stagedConfigValidator
	}
	validations := validate(renderedPaths)
	return written, validations, renderedPaths, nil
}

func sortedRenderedPaths(rendered map[string]string) []string {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
