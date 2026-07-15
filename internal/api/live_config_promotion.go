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
	"github.com/mikkelchokolate/Veil/internal/inbounds"
	"github.com/mikkelchokolate/Veil/internal/protocols"
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

type liveConfigOrphanDir struct {
	subpath string
	ext     string
	exclude string
}

func scanLiveConfigOrphans(liveRoot string, liveFiles []string) ([]string, error) {
	orphaned := []string{}
	activeMap := make(map[string]bool)
	for _, f := range liveFiles {
		activeMap[filepath.Clean(f)] = true
	}

	for _, d := range liveConfigOrphanDirs() {
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

func liveConfigOrphanDirs() []liveConfigOrphanDir {
	dirs := []liveConfigOrphanDir{}
	seen := map[liveConfigOrphanDir]bool{}
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		if !hasTemplateRuntime(plugin) {
			continue
		}
		sub := filepath.ToSlash(cr.ArtifactSpec().Subpath)
		if sub == "" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(sub))
		ext := filepath.Ext(filepath.Base(sub))
		if dir == "." || ext == "" {
			continue
		}
		d := liveConfigOrphanDir{subpath: dir, ext: ext, exclude: filepath.Base(sub)}
		if seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	// Aggregate protocols (e.g. mieru) generate a single config file.
	// Their directories must also be scanned so that the config file becomes
	// orphaned and removed when the protocol is disabled or no longer inbounds.
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		if hasTemplateRuntime(plugin) {
			continue
		}
		if _, ok := protocols.AsRuntimeProvider(plugin); !ok {
			continue
		}
		sub := filepath.ToSlash(cr.ArtifactSpec().Subpath)
		if sub == "" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(sub))
		ext := filepath.Ext(filepath.Base(sub))
		if dir == "." || ext == "" {
			continue
		}
		// For aggregate protocols the main config file itself is eligible for
		// cleanup when it is no longer in the active promotion set, so no
		// exclude file is set.
		d := liveConfigOrphanDir{subpath: dir, ext: ext}
		if seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	// The redesign no longer advertises the old per-inbound Caddy template,
	// so discover its artifacts explicitly during migration/cleanup.
	legacyCaddy := liveConfigOrphanDir{subpath: "caddy", ext: ".Caddyfile"}
	if !seen[legacyCaddy] {
		seen[legacyCaddy] = true
		dirs = append(dirs, legacyCaddy)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].subpath == dirs[j].subpath {
			return dirs[i].ext < dirs[j].ext
		}
		return dirs[i].subpath < dirs[j].subpath
	})
	return dirs
}

func hasTemplateRuntime(plugin protocols.ProtocolPlugin) bool {
	rp, ok := protocols.AsRuntimeProvider(plugin)
	if !ok {
		return false
	}
	for _, descriptor := range rp.RuntimeDescriptors(nil) {
		if descriptor.TemplateUnit != "" || strings.Contains(descriptor.Unit, "@") {
			return true
		}
	}
	return false
}

func UnitForArtifactID(id string) (string, bool) {
	return unitForPath(filepath.ToSlash(id))
}

func UnitForLiveConfig(livePath string) (string, bool) {
	return unitForPath(filepath.ToSlash(livePath))
}

func unitForPath(slashPath string) (string, bool) {
	if strings.HasSuffix(slashPath, ".Caddyfile") {
		name := strings.TrimSuffix(filepath.Base(slashPath), ".Caddyfile")
		if name != "" && inbounds.IsSafeName(name) {
			return "veil-caddy@" + name + ".service", true
		}
	}
	registry := protocols.NewRegistry()

	// Exact match covers aggregated units (mieru) and any artifact whose path
	// equals a plugin's artifact spec subpath.
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		sub := cr.ArtifactSpec().Subpath
		if sub == "" || (slashPath != sub && !strings.HasSuffix(slashPath, "/"+sub)) {
			continue
		}
		rp, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		descs := rp.RuntimeDescriptors(nil)
		if len(descs) > 0 && descs[0].Unit != "" {
			return descs[0].Unit, true
		}
	}

	// Pattern match per-inbound template units from plugin artifact specs.
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		sub := cr.ArtifactSpec().Subpath
		if sub == "" {
			continue
		}
		dir := filepath.Dir(sub)
		var rest string
		if strings.HasPrefix(slashPath, dir+"/") {
			rest = strings.TrimPrefix(slashPath, dir+"/")
		} else {
			marker := "/" + dir + "/"
			idx := strings.LastIndex(slashPath, marker)
			if idx == -1 {
				continue
			}
			rest = slashPath[idx+len(marker):]
		}
		suffix := filepath.Ext(filepath.Base(sub))
		if suffix == "" {
			continue
		}
		if !strings.HasSuffix(rest, suffix) {
			continue
		}
		name := strings.TrimSuffix(rest, suffix)
		if name == "" || strings.Contains(name, "/") || !inbounds.IsSafeName(name) {
			continue
		}
		rp, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		template := ""
		for _, d := range rp.RuntimeDescriptors(nil) {
			if d.TemplateUnit != "" {
				template = d.TemplateUnit
				break
			}
			if strings.Contains(d.Unit, "@") {
				template = d.Unit
				break
			}
		}
		if template == "" {
			continue
		}
		at := strings.Index(template, "@")
		if at == -1 {
			continue
		}
		unit := template[:at+1] + name + template[at+1:]
		return unit, true
	}

	// WARP is not a protocol plugin; handle it explicitly.
	if slashPath == generatedconfig.WarpConfigSubpath || strings.HasSuffix(slashPath, "/"+generatedconfig.WarpConfigSubpath) {
		return renderer.UnitWarp, true
	}
	return "", false
}

func (p LiveConfigPromotion) LivePathForStagedConfig(stagedPath string) (string, bool) {
	return livePathForStagedConfig(p.applyRoot, stagedPath)
}

func livePathForStagedConfig(applyRoot string, stagedPath string) (string, bool) {
	slashPath := filepath.ToSlash(stagedPath)
	slashRoot := strings.TrimRight(filepath.ToSlash(applyRoot), "/")
	prefix := slashRoot + "/generated/"
	if !strings.HasPrefix(slashPath, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(slashPath, prefix)
	cleanRel := filepath.ToSlash(filepath.Clean(rel))
	if cleanRel != rel || cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "../") {
		return "", false
	}
	if !isPromotableGeneratedConfig(cleanRel) {
		return "", false
	}
	return filepath.Join(applyRoot, "live", filepath.FromSlash(cleanRel)), true
}

func isPromotableGeneratedConfig(rel string) bool {
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		sub := filepath.ToSlash(cr.ArtifactSpec().Subpath)
		if sub == "" {
			continue
		}
		if rel == sub || isPromotableDynamicProtocolArtifact(plugin, sub, rel) {
			return true
		}
	}
	return rel == generatedconfig.WarpConfigSubpath
}

func isPromotableDynamicProtocolArtifact(plugin protocols.ProtocolPlugin, sub string, rel string) bool {
	if !hasTemplateRuntime(plugin) {
		return false
	}
	dir := filepath.ToSlash(filepath.Dir(sub))
	if dir == "." || !strings.HasPrefix(rel, dir+"/") {
		return false
	}
	rest := strings.TrimPrefix(rel, dir+"/")
	if strings.Contains(rest, "/") {
		return false
	}
	ext := filepath.Ext(filepath.Base(sub))
	if ext == "" || !strings.HasSuffix(rest, ext) {
		return false
	}
	name := strings.TrimSuffix(rest, ext)
	return inbounds.IsSafeName(name)
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
		rollbackActions = p.reload(rollbackFiles)
	}
	return rollbackFiles, rollbackActions
}
