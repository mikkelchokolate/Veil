package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func publicationArtifactDigests(stagingRoot, liveRoot string, writes, removals []string) (string, string, error) {
	ids := append(append([]string(nil), writes...), removals...)
	sort.Strings(ids)
	expected := sha256.New()
	previous := sha256.New()
	for _, id := range ids {
		if id == "" || filepath.IsAbs(id) || id == ".." || strings.HasPrefix(id, "../") {
			return "", "", fmt.Errorf("invalid publication artifact %q", id)
		}
		if publicationContains(writes, id) {
			digest, err := publicationFileDigest(filepath.Join(stagingRoot, filepath.FromSlash(id)))
			if err != nil {
				return "", "", err
			}
			fmt.Fprintf(expected, "%s\x00%s\n", id, digest)
		} else {
			fmt.Fprintf(expected, "%s\x00<absent>\n", id)
		}
		digest, err := publicationFileDigest(filepath.Join(liveRoot, filepath.FromSlash(id)))
		if err != nil {
			if !os.IsNotExist(err) {
				return "", "", err
			}
			digest = "<absent>"
		}
		fmt.Fprintf(previous, "%s\x00%s\n", id, digest)
	}
	return hex.EncodeToString(expected.Sum(nil)), hex.EncodeToString(previous.Sum(nil)), nil
}

func publicationFileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("publication artifact is not a regular file: %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func publicationContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
