package cli

import (
	"fmt"
	"os"

	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
)

func replaceVeilBinaryFromArchive(currentPath string, archive []byte, yes bool) (string, error) {
	binary, err := updateflow.ExtractVeilBinary(archive)
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}
	backupPath := currentPath + ".backup"
	if err := updateflow.CopyFileData(currentPath, backupPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: %w", err)
	}
	if !yes {
		return backupPath, fmt.Errorf("update requires --yes to confirm replacing %s", currentPath)
	}
	if err := updateflow.ReplaceBinaryAtomic(currentPath, binary); err != nil {
		return backupPath, fmt.Errorf("replace binary: %w", err)
	}
	return backupPath, nil
}
