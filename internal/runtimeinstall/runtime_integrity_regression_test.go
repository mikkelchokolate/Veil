package runtimeinstall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInstallRejectsRuntimeReleaseWhenChecksumAssetIsMissing(t *testing.T) {
	binary := []byte("runtime-without-integrity")
	runtime := Runtime{
		Name:   "unsigned-runtime",
		Binary: "unsigned-runtime",
		Method: MethodRawBinary,
		Repo:   "example/unsigned-runtime",
		AssetMatch: func(name string) bool {
			return name == "unsigned-runtime-linux-amd64"
		},
		ChecksumMatch:  func(string) bool { return false },
		Version:        "v1.2.3",
		Integrity:      "upstream-checksum",
		VersionArgs:    []string{"--version"},
		VersionCommand: "test-runtime --version",
		VersionPattern: `1\.2\.3`,
	}
	result := Install(context.Background(), Options{
		BinDir: t.TempDir(),
		Arch:   "amd64",
		FetchRelease: func(context.Context, string) (*Release, error) {
			return &Release{TagName: "v1.2.3", Assets: []Asset{{Name: "unsigned-runtime-linux-amd64", BrowserDownloadURL: "https://example/runtime"}}}, nil
		},
		Download: func(context.Context, string) ([]byte, error) { return binary, nil },
	}, runtime)
	if result.Err == nil {
		t.Fatal("runtime was installed although the mandatory checksum asset was absent")
	}
}

func TestInstallRejectsInstalledBinaryVersionMismatchBeforePublication(t *testing.T) {
	expectedTag := "v1.2.3"
	binary := []byte("#!/bin/sh\necho 'unsigned-runtime v9.9.9'\n")
	assetName := "runtime-linux-amd64"
	checksums := []byte(fmt.Sprintf("%s  %s\n", sha256Hex(binary), assetName))
	binDir := t.TempDir()
	runtime := Runtime{
		Name:           "versioned-runtime",
		Binary:         "versioned-runtime",
		Method:         MethodRawBinary,
		Repo:           "example/versioned-runtime",
		Version:        expectedTag,
		Integrity:      "upstream-checksum",
		VersionArgs:    []string{"--version"},
		VersionCommand: "test-runtime --version",
		VersionPattern: `1\.2\.3`,
		AssetMatch: func(name string) bool {
			return name == assetName
		},
		ChecksumMatch: func(name string) bool { return name == "checksums.txt" },
	}
	result := Install(context.Background(), Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(context.Context, string) (*Release, error) {
			return &Release{TagName: expectedTag, Assets: []Asset{
				{Name: assetName, BrowserDownloadURL: "https://example/runtime"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://example/checksums"},
			}}, nil
		},
		Download: func(_ context.Context, url string) ([]byte, error) {
			if url == "https://example/runtime" {
				return binary, nil
			}
			return checksums, nil
		},
	}, runtime)
	if result.Err == nil {
		t.Fatal("runtime with mismatching --version output was published")
	}
	if _, err := os.Stat(filepath.Join(binDir, runtime.Binary)); !os.IsNotExist(err) {
		t.Fatalf("mismatching binary must not be published, stat err=%v", err)
	}
}

func TestSuccessfulRuntimeInstallRecordsActualDigestAndVerifiedUpstreamVersion(t *testing.T) {
	binary := []byte("#!/bin/sh\necho 'versioned-runtime v1.2.3'\n")
	assetName := "runtime-linux-amd64"
	checksums := []byte(fmt.Sprintf("%s  %s\n", sha256Hex(binary), assetName))
	runtime := Runtime{
		Name:           "versioned-runtime",
		Binary:         "versioned-runtime",
		Method:         MethodRawBinary,
		Repo:           "example/versioned-runtime",
		Version:        "v1.2.3",
		Integrity:      "upstream-checksum",
		VersionArgs:    []string{"--version"},
		VersionCommand: "test-runtime --version",
		VersionPattern: `1\.2\.3`,
		AssetMatch:     func(name string) bool { return name == assetName },
		ChecksumMatch:  func(name string) bool { return name == "checksums.txt" },
	}
	result := Install(context.Background(), Options{
		BinDir: t.TempDir(),
		Arch:   "amd64",
		FetchRelease: func(context.Context, string) (*Release, error) {
			return &Release{TagName: "v1.2.3", Assets: []Asset{
				{Name: assetName, BrowserDownloadURL: "https://example/runtime"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://example/checksums"},
			}}, nil
		},
		Download: func(_ context.Context, url string) ([]byte, error) {
			if url == "https://example/runtime" {
				return binary, nil
			}
			return checksums, nil
		},
		RunVersion: fixedRuntimeVersion("versioned-runtime v1.2.3"),
	}, runtime)
	if result.Err != nil {
		t.Fatalf("precondition install failed: %v", result.Err)
	}
	value := reflect.ValueOf(result)
	for _, field := range []string{"SHA256", "VerifiedVersion"} {
		member := value.FieldByName(field)
		if !member.IsValid() || member.Kind() != reflect.String || member.String() == "" {
			t.Errorf("install result does not record %s", field)
		}
	}
}
