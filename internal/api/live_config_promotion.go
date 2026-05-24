package api

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/veil-panel/veil/internal/atomicfile"
	"github.com/veil-panel/veil/internal/generatedconfig"
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
	sort.Strings(liveFiles)
	sort.Strings(backupFiles)
	sort.Slice(records, func(i, j int) bool { return records[i].LivePath < records[j].LivePath })
	return liveFiles, backupFiles, records, nil
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
