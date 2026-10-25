//go:build windows

package veilenv

import "os"

func preserveOwner(string, os.FileInfo) error { return nil }
