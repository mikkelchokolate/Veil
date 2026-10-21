//go:build !unix

package api

import "os"

// matchSessionStoreOwner is a no-op where POSIX ownership does not apply.
func matchSessionStoreOwner(dir, file *os.File) error { return nil }
