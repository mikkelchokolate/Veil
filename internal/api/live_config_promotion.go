package api

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type LiveConfigPromotion struct {
	applyRoot string
	reload    func([]string) []ServiceActionResult
}

func NewLiveConfigPromotion(applyRoot string, reload func([]string) []ServiceActionResult) LiveConfigPromotion {
	return LiveConfigPromotion{applyRoot: applyRoot, reload: reload}
}

func (p LiveConfigPromotion) Promote(stagedPaths []string) ([]string, []string, []livePromotionRecord, error) {
	liveFiles := []string{}
	backupFiles := []string{}
	records := []livePromotionRecord{}
	backupRoot := filepath.Join(p.applyRoot, "backups", time.Now().UTC().Format("20060102T150405.000000000Z"))
	for _, stagedPath := range stagedPaths {
		livePath, ok := p.LivePathForStagedConfig(stagedPath)
		if !ok {
			continue
		}
		body, err := os.ReadFile(stagedPath)
		if err != nil {
			return nil, nil, nil, err
		}
		record := livePromotionRecord{LivePath: livePath}
		if existing, err := os.ReadFile(livePath); err == nil {
			relPath := strings.TrimPrefix(livePath, filepath.VolumeName(livePath))
			backupPath := filepath.Join(backupRoot, strings.TrimPrefix(filepath.ToSlash(relPath), "/"))
			if err := atomicfile.Write(backupPath, existing, 0o600, 0o700); err != nil {
				return nil, nil, nil, err
			}
			record.HadPrevious = true
			record.BackupPath = backupPath
			backupFiles = append(backupFiles, backupPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, err
		}
		if err := atomicfile.Write(livePath, body, 0o600, 0o700); err != nil {
			return nil, nil, nil, err
		}
		liveFiles = append(liveFiles, livePath)
		records = append(records, record)
	}

	orphans, err := p.scanOrphans(liveFiles)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, orphanPath := range orphans {
		body, err := os.ReadFile(orphanPath)
		if err != nil {
			continue
		}
		relPath := strings.TrimPrefix(orphanPath, filepath.VolumeName(orphanPath))
		backupPath := filepath.Join(backupRoot, strings.TrimPrefix(filepath.ToSlash(relPath), "/"))
		if err := atomicfile.Write(backupPath, body, 0o600, 0o700); err != nil {
			return nil, nil, nil, err
		}
		if err := os.Remove(orphanPath); err != nil && !os.IsNotExist(err) {
			return nil, nil, nil, err
		}
		record := livePromotionRecord{
			LivePath:    orphanPath,
			BackupPath:  backupPath,
			HadPrevious: true,
		}
		backupFiles = append(backupFiles, backupPath)
		records = append(records, record)
	}

	sort.Strings(liveFiles)
	sort.Strings(backupFiles)
	sort.Slice(records, func(i, j int) bool { return records[i].LivePath < records[j].LivePath })
	return liveFiles, backupFiles, records, nil
}

func (p LiveConfigPromotion) scanOrphans(liveFiles []string) ([]string, error) {
	return scanLiveConfigOrphans(filepath.Join(p.applyRoot, "live"), liveFiles)
}

func scanLiveConfigOrphans(liveRoot string, liveFiles []string) ([]string, error) {
	orphaned := []string{}
	activeMap := make(map[string]bool)
	for _, f := range liveFiles {
		activeMap[filepath.Clean(f)] = true
	}

	dirs := []struct {
		subpath string
		ext     string
		exclude string
	}{
		{subpath: "caddy", ext: ".Caddyfile", exclude: "panel.Caddyfile"},
		{subpath: "hysteria2", ext: ".yaml", exclude: "server.yaml"},
		{subpath: "olcrtc", ext: ".yaml", exclude: "server.yaml"},
	}

	for _, d := range dirs {
		dirPath := filepath.Join(liveRoot, d.subpath)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			// Skip missing dirs, and root-owned generated subdirs the panel
			// can't read: they hold no panel-removable orphans, so a permission
			// error there must not abort the whole apply.
			if os.IsNotExist(err) || os.IsPermission(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, d.ext) {
				continue
			}
			if name == d.exclude {
				continue
			}
			absPath := filepath.Join(dirPath, name)
			if !activeMap[filepath.Clean(absPath)] {
				orphaned = append(orphaned, absPath)
			}
		}
	}
	return orphaned, nil
}

func UnitForArtifactID(id string) (string, bool) {
	slashPath := filepath.ToSlash(id)
	// WARP is a single, fixed config (not a per-inbound instance): removing it
	// (operator disables WARP) must stop and disable the sing-box unit.
	if slashPath == generatedconfig.WarpConfigSubpath {
		return renderer.UnitWarp, true
	}
	for _, spec := range []struct {
		prefix  string
		suffix  string
		unit    string
		exclude string
	}{
		{prefix: "caddy/", suffix: ".Caddyfile", unit: "veil-caddy@", exclude: "panel"},
		{prefix: "hysteria2/", suffix: ".yaml", unit: "veil-hysteria2@", exclude: "server"},
		{prefix: "olcrtc/", suffix: ".yaml", unit: "veil-olcrtc@", exclude: "server"},
	} {
		if !strings.HasPrefix(slashPath, spec.prefix) || !strings.HasSuffix(slashPath, spec.suffix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(slashPath, spec.prefix), spec.suffix)
		if name != "" && name != spec.exclude && !strings.Contains(name, "/") {
			return spec.unit + name + ".service", true
		}
	}
	return "", false
}

func UnitForLiveConfig(livePath string) (string, bool) {
	slashPath := filepath.ToSlash(livePath)
	if idx := strings.Index(slashPath, "/live/caddy/"); idx != -1 {
		name := strings.TrimSuffix(slashPath[idx+len("/live/caddy/"):], ".Caddyfile")
		if name != "panel" {
			return "veil-caddy@" + name + ".service", true
		}
	}
	if idx := strings.Index(slashPath, "/live/hysteria2/"); idx != -1 {
		name := strings.TrimSuffix(slashPath[idx+len("/live/hysteria2/"):], ".yaml")
		if name != "server" {
			return "veil-hysteria2@" + name + ".service", true
		}
	}
	if idx := strings.Index(slashPath, "/live/olcrtc/"); idx != -1 {
		name := strings.TrimSuffix(slashPath[idx+len("/live/olcrtc/"):], ".yaml")
		if name != "server" {
			return "veil-olcrtc@" + name + ".service", true
		}
	}
	return "", false
}

func (p LiveConfigPromotion) LivePathForStagedConfig(stagedPath string) (string, bool) {
	return generatedconfig.NewArtifactCatalog().LivePathForStagedConfig(p.applyRoot, stagedPath)
}

func (p LiveConfigPromotion) Rollback(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	rollbackFiles := []string{}
	for _, record := range records {
		if record.HadPrevious {
			body, err := os.ReadFile(record.BackupPath)
			if err != nil {
				continue
			}
			if err := atomicfile.Write(record.LivePath, body, 0o600, 0o700); err != nil {
				continue
			}
			rollbackFiles = append(rollbackFiles, record.LivePath)
			continue
		}
		if err := os.Remove(record.LivePath); err == nil || errors.Is(err, os.ErrNotExist) {
			rollbackFiles = append(rollbackFiles, record.LivePath)
		}
	}
	sort.Strings(rollbackFiles)
	rollbackActions := []ServiceActionResult{}
	if len(rollbackFiles) > 0 && p.reload != nil {
		rollbackActions = p.reload(liveFiles)
	}
	return rollbackFiles, rollbackActions
}
