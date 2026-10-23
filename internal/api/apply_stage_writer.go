package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
	// Render, when set, runs after routing dat is on disk so protocol configs
	// that stat geoip/geosite (Hysteria2 ACL) see the files this apply fetched.
	Render   func() (map[string]string, error)
	Validate func([]string) []ConfigValidationResult
}

func WriteApplyStage(input ApplyStageInput) ([]string, []ConfigValidationResult, []string, error) {
	written, validations, renderedPaths, _, err := writeApplyStage(input)
	return written, validations, renderedPaths, err
}

// writeApplyStage is WriteApplyStage that also returns the SHA-256 of every
// staged file it wrote, keyed by cleaned staged path. Each digest is taken
// from the in-memory bytes this process rendered or verified, never from a
// re-read of the veil-writable staging tree, so the promote request can bind
// the privileged helper to exactly those bytes (#1269).
func writeApplyStage(input ApplyStageInput) ([]string, []ConfigValidationResult, []string, map[string]string, error) {
	stageDir := filepath.Join(input.ApplyRoot, "generated", "veil")
	planPath := filepath.Join(stageDir, "apply-plan.json")
	statePath := filepath.Join(stageDir, "management-state.json")
	planBody, err := json.MarshalIndent(input.Plan, "", "  ")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := atomicfile.Write(planPath, append(planBody, '\n'), 0o600, 0o700); err != nil {
		return nil, nil, nil, nil, err
	}
	snapshotBody, err := managementstate.NewStore("", input.Cipher).Marshal(input.Snapshot)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := atomicfile.Write(statePath, snapshotBody, 0o600, 0o700); err != nil {
		return nil, nil, nil, nil, err
	}
	written := []string{planPath, statePath}
	routingSource := input.RoutingSource
	if routeDatSourceTransform != nil {
		routingSource = routeDatSourceTransform(routingSource)
	}
	routingMaterial := generatedconfig.NewRoutingSourceMaterial(input.ApplyRoot, routingSource).
		WithContextDownloader(routeDatDownloader).WithContext(input.Context)
	if routeDatSignatureVerifier != nil {
		routingMaterial = routingMaterial.WithSignatureVerifier(routeDatSignatureVerifier)
	}
	routingFiles, digests, err := routingMaterial.WriteGeneratedWithDigests()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	written = append(written, routingFiles...)
	rendered := input.Rendered
	if input.Render != nil {
		rendered, err = input.Render()
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	renderedPaths := sortedRenderedPaths(rendered)
	// Every path is checked before any rendered body is written, so one bad
	// path cannot leave a partially written set behind (#1273).
	for _, path := range renderedPaths {
		if err := validateStagedRenderedPath(input.ApplyRoot, path); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	for _, path := range renderedPaths {
		body := []byte(rendered[path])
		if err := atomicfile.Write(path, body, 0o600, 0o700); err != nil {
			return nil, nil, nil, nil, err
		}
		written = append(written, path)
		digests[filepath.Clean(path)] = stagedBodyDigest(body)
	}
	validate := input.Validate
	if validate == nil {
		validate = stagedConfigValidator
	}
	validations := validate(renderedPaths)
	return written, validations, renderedPaths, digests, nil
}

// validateStagedRenderedPath confines a renderer-supplied path to the
// staging tree. Renderers only emit fixed artifact subpaths or names derived
// from inbounds.IsSafeName, and promotion re-checks every path, but the write
// itself happens here, before promotion, as the veil uid. So a renderer that
// ever emits a `..` or absolute subpath must not be able to write outside
// <applyRoot>/generated (#1273). The path must be canonical (filepath.Clean
// leaves it unchanged), strictly beneath <applyRoot>/generated, and outside
// generated/veil. That directory holds this writer's own plan and state
// files, which a rendered artifact must not overwrite.
func validateStagedRenderedPath(applyRoot, path string) error {
	if filepath.Clean(path) != path {
		return fmt.Errorf("rendered config path %q is not canonical", path)
	}
	generatedRoot := filepath.Join(applyRoot, "generated")
	if filepath.IsAbs(generatedRoot) != filepath.IsAbs(path) {
		return fmt.Errorf("rendered config path %q is outside the staging root %q", path, generatedRoot)
	}
	rel, err := filepath.Rel(generatedRoot, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("rendered config path %q is outside the staging root %q", path, generatedRoot)
	}
	if rel == "veil" || strings.HasPrefix(rel, "veil"+string(filepath.Separator)) {
		return fmt.Errorf("rendered config path %q targets the reserved stage directory", path)
	}
	return nil
}

func stagedBodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func sortedRenderedPaths(rendered map[string]string) []string {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
