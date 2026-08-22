package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

var routeDatDownloader generatedconfig.RoutingSourceContextDownloader = generatedconfig.DownloadRouteDatContext
var routeDatSignatureVerifier generatedconfig.RoutingSourceSignatureVerifier
var routeDatSourceTransform func(RoutingSource) RoutingSource

type ApplyStageInput struct {
	Context       context.Context
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
	routingSource := input.RoutingSource
	if routeDatSourceTransform != nil {
		routingSource = routeDatSourceTransform(routingSource)
	}
	routingMaterial := generatedconfig.NewRoutingSourceMaterial(input.ApplyRoot, routingSource).
		WithContextDownloader(routeDatDownloader).WithContext(input.Context)
	if routeDatSignatureVerifier != nil {
		routingMaterial = routingMaterial.WithSignatureVerifier(routeDatSignatureVerifier)
	}
	routingFiles, err := routingMaterial.WriteGenerated()
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
