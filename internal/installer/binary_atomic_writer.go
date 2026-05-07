package installer

import (
	"os"
	"path/filepath"
)

type BinaryAtomicWriter struct{}

func NewBinaryAtomicWriter() BinaryAtomicWriter { return BinaryAtomicWriter{} }

func (BinaryAtomicWriter) Write(destination string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp := destination + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
