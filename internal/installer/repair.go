package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/veil-panel/veil/internal/renderer"
)

type RepairReason string

const (
	RepairReasonMissing RepairReason = "missing"
	RepairReasonDrifted RepairReason = "drifted"
)

type RepairAction struct {
	Path    string
	Reason  RepairReason
	Content string
	Mode    os.FileMode
}

type RepairPlan struct {
	Actions []RepairAction
}

type BinaryRepairAction struct {
	Name        string
	URL         string
	Destination string
	SHA256      string
	Reason      RepairReason
}

type BinaryRepairPlan struct {
	Actions []BinaryRepairAction
}

type RepairResult struct {
	WrittenFiles []string
}

func BuildRepairPlan(profile RURecommendedProfile, paths ApplyPaths) (RepairPlan, error) {
	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		return RepairPlan{}, err
	}
	plan := RepairPlan{}
	for _, file := range files {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			if isMissingOrBlocked(err) {
				plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonMissing, Content: file.Content, Mode: file.Mode})
				continue
			}
			return RepairPlan{}, err
		}
		if string(body) != file.Content {
			plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonDrifted, Content: file.Content, Mode: file.Mode})
		}
	}
	return plan, nil
}

func (p RepairPlan) HasChanges() bool {
	return len(p.Actions) > 0
}

func (p RepairPlan) Summary() string {
	if len(p.Actions) == 0 {
		return "No repair actions required\n"
	}
	var b strings.Builder
	for _, action := range p.Actions {
		fmt.Fprintf(&b, "repair %s %s\n", action.Reason, action.Path)
	}
	return b.String()
}

func BuildBinaryRepairPlan(binary BinaryAcquisition) (BinaryRepairPlan, error) {
	if strings.TrimSpace(binary.Name) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary name is required")
	}
	if strings.TrimSpace(binary.URL) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary url is required")
	}
	if strings.TrimSpace(binary.Destination) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary destination is required")
	}
	if strings.TrimSpace(binary.SHA256) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("sha256 checksum is required for binary repair")
	}
	body, err := os.ReadFile(binary.Destination)
	if os.IsNotExist(err) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonMissing}}}, nil
	}
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	actual, err := SHA256Hex(body)
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	if actual != strings.ToLower(strings.TrimSpace(binary.SHA256)) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonDrifted}}}, nil
	}
	return BinaryRepairPlan{}, nil
}

func (p BinaryRepairPlan) Summary() string {
	if len(p.Actions) == 0 {
		return "No binary repair actions required\n"
	}
	var b strings.Builder
	for _, action := range p.Actions {
		fmt.Fprintf(&b, "repair %s binary %s -> %s\n", action.Reason, action.Name, action.Destination)
	}
	return b.String()
}

func ApplyRepairPlan(plan RepairPlan) (RepairResult, error) {
	result := RepairResult{}
	for _, action := range plan.Actions {
		if err := writeManagedFile(action.Path, action.Content, action.Mode); err != nil {
			return RepairResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, action.Path)
	}
	return result, nil
}

type managedFile struct {
	Path    string
	Content string
	Mode    os.FileMode
}

func desiredManagedFiles(profile RURecommendedProfile, paths ApplyPaths) ([]managedFile, error) {
	if paths.EtcDir == "" {
		return nil, fmt.Errorf("etc dir is required")
	}
	if paths.VarDir == "" {
		return nil, fmt.Errorf("var dir is required")
	}
	files := []managedFile{}
	if profile.InstallNaive {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"), Content: profile.Caddyfile, Mode: 0o600})
		files = append(files, managedFile{Path: filepath.Join(paths.VarDir, "www", "index.html"), Content: fallbackIndexHTML(profile.Domain), Mode: 0o644})
	}
	if profile.InstallHysteria2 {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "generated", "hysteria2", "server.yaml"), Content: profile.Hysteria2YAML, Mode: 0o600})
	}
	if profile.PanelAuthToken != "" {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "veil.env"), Content: "VEIL_API_TOKEN=" + profile.PanelAuthToken + "\n", Mode: 0o600})
	}
	if paths.SystemdDir != "" {
		units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: paths.EtcDir})
		unitNames := []string{"veil.service"}
		if profile.InstallNaive {
			unitNames = append(unitNames, "veil-naive.service")
		}
		if profile.InstallHysteria2 {
			unitNames = append(unitNames, "veil-hysteria2.service")
		}
		for _, name := range unitNames {
			files = append(files, managedFile{Path: filepath.Join(paths.SystemdDir, name), Content: units[name], Mode: 0o644})
		}
	}
	return files, nil
}

// isMissingOrBlocked reports whether err means the file cannot be read
// because it doesn't exist or a path component is not a directory (ENOTDIR).
func isMissingOrBlocked(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
