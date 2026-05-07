package cli

import (
	"fmt"
	"os"
)

func replaceVeilBinaryFromArchive(currentPath string, archive []byte, yes bool) (string, error) {
	binary, err := extractVeilBinary(archive)
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}
	backupPath := currentPath + ".backup"
	if err := copyFileData(currentPath, backupPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: %w", err)
	}
	if !yes {
		return backupPath, fmt.Errorf("update requires --yes to confirm replacing %s", currentPath)
	}
	if err := replaceBinaryAtomic(currentPath, binary); err != nil {
		return backupPath, fmt.Errorf("replace binary: %w", err)
	}
	return backupPath, nil
}
