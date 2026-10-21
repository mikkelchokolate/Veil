//go:build !unix

package api

import "os"

// lockSessionStore is a no-op where flock is unavailable: the root CLI and a
// service-account Panel sharing one store is a Linux deployment shape.
func lockSessionStore(path string) (*os.File, error) { return nil, nil }

func unlockSessionStore(file *os.File) {}
