package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var archiveNamePattern = regexp.MustCompile(`^veil_backup_(\d{8})_(\d{6})\.tar\.gz(?:\.enc)?$`)

type ArchiveEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	Encrypted bool      `json:"encrypted"`
}

type RetentionPolicy struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

type PruneResult struct {
	Kept    []string `json:"kept"`
	Deleted []string `json:"deleted"`
	DryRun  bool     `json:"dryRun"`
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{Daily: 7, Weekly: 4, Monthly: 12}
}

func ListArchives(dir string) ([]ArchiveEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []ArchiveEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}
	archives := make([]ArchiveEntry, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		createdAt, ok := parseArchiveTimestamp(entry.Name())
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat backup archive %s: %w", entry.Name(), err)
		}
		archives = append(archives, ArchiveEntry{
			Name:      entry.Name(),
			Path:      filepath.Join(dir, entry.Name()),
			Size:      info.Size(),
			CreatedAt: createdAt,
			Encrypted: strings.HasSuffix(entry.Name(), ".enc"),
		})
	}
	sort.Slice(archives, func(i, j int) bool {
		if archives[i].CreatedAt.Equal(archives[j].CreatedAt) {
			return archives[i].Name > archives[j].Name
		}
		return archives[i].CreatedAt.After(archives[j].CreatedAt)
	})
	return archives, nil
}

func PruneArchives(dir string, policy RetentionPolicy, dryRun bool) (PruneResult, error) {
	if policy.Daily < 0 || policy.Weekly < 0 || policy.Monthly < 0 {
		return PruneResult{}, errors.New("retention counts cannot be negative")
	}
	archives, err := ListArchives(dir)
	if err != nil {
		return PruneResult{}, err
	}
	keep := make(map[string]bool)
	selectRetentionBuckets(archives, policy.Daily, func(entry ArchiveEntry) string {
		return entry.CreatedAt.Format("2006-01-02")
	}, keep)
	selectRetentionBuckets(archives, policy.Weekly, func(entry ArchiveEntry) string {
		year, week := entry.CreatedAt.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	}, keep)
	selectRetentionBuckets(archives, policy.Monthly, func(entry ArchiveEntry) string {
		return entry.CreatedAt.Format("2006-01")
	}, keep)

	result := PruneResult{DryRun: dryRun}
	for _, entry := range archives {
		if keep[entry.Name] {
			result.Kept = append(result.Kept, entry.Name)
			continue
		}
		result.Deleted = append(result.Deleted, entry.Name)
		if !dryRun {
			if err := os.Remove(entry.Path); err != nil {
				return result, fmt.Errorf("remove backup archive %s: %w", entry.Name, err)
			}
		}
	}
	return result, nil
}

func selectRetentionBuckets(
	archives []ArchiveEntry,
	limit int,
	bucket func(ArchiveEntry) string,
	keep map[string]bool,
) {
	if limit <= 0 {
		return
	}
	selected := make(map[string]bool)
	for _, entry := range archives {
		key := bucket(entry)
		if selected[key] {
			continue
		}
		if len(selected) >= limit {
			break
		}
		selected[key] = true
		keep[entry.Name] = true
	}
}

func parseArchiveTimestamp(name string) (time.Time, bool) {
	match := archiveNamePattern.FindStringSubmatch(name)
	if len(match) != 3 {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("20060102_150405", match[1]+"_"+match[2], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
