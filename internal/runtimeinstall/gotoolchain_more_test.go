package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseGoVersionCases(t *testing.T) {
	cases := []struct {
		in   string
		want goVersion
		ok   bool
	}{
		{"go version go1.26.4 linux/amd64", goVersion{1, 26, 4}, true},
		{"1.26.4", goVersion{1, 26, 4}, true},
		{"go1.25 linux/amd64", goVersion{1, 25, 0}, true},
		{"no version here", goVersion{}, false},
	}
	for _, c := range cases {
		got, ok := parseGoVersion(c.in)
		if ok != c.ok {
			t.Errorf("parseGoVersion(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseGoVersion(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestGoVersionAtLeast(t *testing.T) {
	cases := []struct {
		v, other goVersion
		want     bool
	}{
		{goVersion{1, 26, 4}, goVersion{1, 26, 4}, true},
		{goVersion{1, 26, 4}, goVersion{1, 26, 3}, true},
		{goVersion{1, 26, 4}, goVersion{1, 25, 99}, true},
		{goVersion{1, 26, 4}, goVersion{1, 27, 0}, false},
		{goVersion{2, 0, 0}, goVersion{1, 99, 99}, true},
		{goVersion{1, 0, 0}, goVersion{2, 0, 0}, false},
	}
	for _, c := range cases {
		if got := c.v.atLeast(c.other); got != c.want {
			t.Errorf("%+v.atLeast(%+v) = %v, want %v", c.v, c.other, got, c.want)
		}
	}
}

func TestKnownGoBins(t *testing.T) {
	t.Setenv("GOROOT", "/fake/goroot")
	bins := knownGoBins()
	seen := map[string]int{}
	for _, b := range bins {
		seen[b]++
	}
	for b, n := range seen {
		if n > 1 {
			t.Errorf("duplicate candidate %q", b)
		}
	}
	if seen["/fake/goroot/bin/go"] != 1 {
		t.Errorf("expected GOROOT bin, got candidates: %v", bins)
	}
}

func TestKnownGoBinsIncludesHome(t *testing.T) {
	t.Setenv("HOME", "/fakehome")
	bins := knownGoBins()
	hasHome := false
	for _, b := range bins {
		if b == "/fakehome/go/bin/go" {
			hasHome = true
		}
	}
	if !hasHome {
		t.Error("expected home dir candidate")
	}
}

// makeFakeGoBin writes a fake `go` binary named `dir/go` that prints output
// for version queries. Use a high version (e.g. "go version go1.99.0 linux/amd64")
// to outrank any real system Go present on the test machine.
func makeFakeGoBin(t *testing.T, dir, output string) string {
	t.Helper()
	p := filepath.Join(dir, "go")
	script := fmt.Sprintf("#!/usr/bin/env bash\n%s\nexit 0\n", output)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindBestGo(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = makeFakeGoBin(t, oldDir, "echo 'go version go1.20.0 linux/amd64'")
	want := makeFakeGoBin(t, newDir, "echo 'go version go1.99.0 linux/amd64'")
	t.Setenv("PATH", newDir+":"+oldDir+":"+os.Getenv("PATH"))

	best, err := findBestGo(defaultGoVersion)
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	if best != want {
		t.Fatalf("findBestGo = %q, want %q", best, want)
	}
}

func TestFindBestGoInvalidMinVersion(t *testing.T) {
	_, err := findBestGo("not-a-version")
	if err == nil {
		t.Fatal("expected error for invalid min version")
	}
}

func TestFindBestGoNoSuitable(t *testing.T) {
	dir := t.TempDir()
	makeFakeGoBin(t, dir, "echo 'go version go1.20.0 linux/amd64'")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	// Use a minimum version higher than any system Go to exercise the
	// "no suitable toolchain" branch regardless of the host environment.
	best, err := findBestGo("1.99.0")
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	if best != "" {
		t.Fatalf("expected empty, got %q", best)
	}
}

func TestGoLocalEnv(t *testing.T) {
	t.Setenv("GOROOT", "/fake")
	t.Setenv("GOPATH", "/fakepath")
	t.Setenv("GOTOOLCHAIN", "auto")
	env := goLocalEnv()
	hasLocal := false
	hasGoroot := false
	hasGopath := false
	for _, e := range env {
		if e == "GOTOOLCHAIN=local" {
			hasLocal = true
		}
		if strings.HasPrefix(e, "GOROOT=") {
			hasGoroot = true
		}
		if strings.HasPrefix(e, "GOPATH=") {
			hasGopath = true
		}
	}
	if !hasLocal {
		t.Error("expected GOTOOLCHAIN=local")
	}
	if hasGoroot {
		t.Error("expected GOROOT stripped")
	}
	if hasGopath {
		t.Error("expected GOPATH stripped")
	}
}

func TestGoBuildEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("GOPROXY", "https://proxy")
	env := goBuildEnv("/usr/local/go/bin/go")
	m := map[string]string{}
	for _, e := range env {
		if i := strings.Index(e, "="); i > 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	if m["GOROOT"] != "/usr/local/go" {
		t.Errorf("GOROOT = %q", m["GOROOT"])
	}
	if m["GOBIN"] != "/usr/local/go/bin" {
		t.Errorf("GOBIN = %q", m["GOBIN"])
	}
	if !strings.Contains(m["PATH"], "/usr/local/go/bin") {
		t.Errorf("PATH = %q", m["PATH"])
	}
	if m["GOPROXY"] != "https://proxy" {
		t.Errorf("GOPROXY = %q", m["GOPROXY"])
	}
	if m["GOTOOLCHAIN"] != "local" {
		t.Errorf("GOTOOLCHAIN = %q", m["GOTOOLCHAIN"])
	}
}

func TestGoBuildEnvPreservesExisting(t *testing.T) {
	t.Setenv("GOROOT", "/old")
	t.Setenv("GOBIN", "/oldbin")
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("GONOSUMDB", "example.com")
	t.Setenv("GOPROXY", "https://proxy")
	env := goBuildEnv("/usr/local/go/bin/go")
	m := map[string]string{}
	for _, e := range env {
		if i := strings.Index(e, "="); i > 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	if m["GOROOT"] != "/usr/local/go" {
		t.Errorf("GOROOT = %q", m["GOROOT"])
	}
	if m["GOBIN"] != "/usr/local/go/bin" {
		t.Errorf("GOBIN = %q", m["GOBIN"])
	}
	if m["GONOSUMDB"] != "example.com" {
		t.Errorf("GONOSUMDB = %q", m["GONOSUMDB"])
	}
	if m["GOPROXY"] != "https://proxy" {
		t.Errorf("GOPROXY = %q", m["GOPROXY"])
	}
}

func TestGoBuildEnvHandlesWindowsPath(t *testing.T) {
	t.Setenv("Path", "/usr/bin")
	env := goBuildEnv("/usr/local/go/bin/go")
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "Path=") || strings.HasPrefix(e, "PATH=") {
			hasPath = true
		}
	}
	if !hasPath {
		t.Error("expected Path preserved")
	}
}

func TestResolveGoFindsSystemGo(t *testing.T) {
	dir := t.TempDir()
	p := makeFakeGoBin(t, dir, "echo 'go version go1.99.0 linux/amd64'")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	got, err := resolveGo(context.Background(), t.TempDir(), nil)
	if err != nil || got != p {
		t.Fatalf("resolveGo = %q, err = %v", got, err)
	}
}

func TestGoToolchainEnsureAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	goDir := filepath.Join(dir, "go"+defaultGoVersion)
	goBin := filepath.Join(goDir, "bin", "go")
	if err := os.MkdirAll(filepath.Dir(goBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goBin, []byte("go"), 0o755); err != nil {
		t.Fatal(err)
	}
	gt := NewGoToolchain(dir)
	got, err := gt.Ensure(context.Background())
	if err != nil || got != goBin {
		t.Fatalf("Ensure = %q, err = %v", got, err)
	}
}

func buildFakeGoTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{
		"go/bin/go":    []byte("#!/bin/sh\necho go version go1.26.4 linux/amd64\n"),
		"go/README.md": []byte("go"),
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGoToolchainEnsureDownloadsAndExtracts(t *testing.T) {
	dir := t.TempDir()
	tarball := buildFakeGoTarball(t)
	sum := sha256.Sum256(tarball)
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])
	defer func() { defaultGoSHA256[key] = oldSHA }()

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}

	goBin, err := gt.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	body, err := os.ReadFile(goBin)
	if err != nil {
		t.Fatalf("read go binary: %v", err)
	}
	if !bytes.Contains(body, []byte("go version")) {
		t.Fatalf("unexpected go binary content: %q", body)
	}
}

func TestGoToolchainDownloadUnsupportedPlatform(t *testing.T) {
	gt := NewGoToolchain(t.TempDir())
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	delete(defaultGoSHA256, key)
	defer func() { defaultGoSHA256[key] = oldSHA }()
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no prebuilt Go") {
		t.Fatalf("expected unsupported platform error, got %v", err)
	}
}

func TestGoToolchainDownloadChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defaultGoSHA256[key] = strings.Repeat("0", 64)
	defer func() { defaultGoSHA256[key] = oldSHA }()

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("wrong content")), Request: req}, nil
	}}}

	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestGoToolchainDownloadBadStatus(t *testing.T) {
	gt := NewGoToolchain(t.TempDir())
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Status: "500 Internal Server Error", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "download Go") {
		t.Fatalf("expected download error, got %v", err)
	}
}

func TestGoToolchainDownloadNetworkError(t *testing.T) {
	gt := NewGoToolchain(t.TempDir())
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestGoToolchainDownloadBadGzip(t *testing.T) {
	gt := NewGoToolchain(t.TempDir())
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	sum := sha256.Sum256([]byte("not gzip"))
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])
	defer func() { defaultGoSHA256[key] = oldSHA }()

	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not gzip")), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gzip Go") {
		t.Fatalf("expected gzip error, got %v", err)
	}
}

func buildFakeGoTarballWithBadPath(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("x")
	if err := tw.WriteHeader(&tar.Header{Name: "../bad", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGoToolchainDownloadInvalidTarPath(t *testing.T) {
	dir := t.TempDir()
	tarball := buildFakeGoTarballWithBadPath(t)
	sum := sha256.Sum256(tarball)
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])
	defer func() { defaultGoSHA256[key] = oldSHA }()

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid path") {
		t.Fatalf("expected invalid path error, got %v", err)
	}
}

func TestKnownGoBinsDeduplicatesDefaultPaths(t *testing.T) {
	t.Setenv("GOROOT", "/usr/local/go")
	bins := knownGoBins()
	seen := map[string]int{}
	for _, b := range bins {
		seen[b]++
	}
	if seen["/usr/local/go/bin/go"] != 1 {
		t.Fatalf("expected exactly one /usr/local/go/bin/go, got %v", seen)
	}
}

func TestFindBestGoVersionCommandFails(t *testing.T) {
	old := knownGoBinsFn
	defer func() { knownGoBinsFn = old }()

	dir := t.TempDir()
	badGo := filepath.Join(dir, "go")
	script := "#!/usr/bin/env bash\necho 'go: cannot find version' >&2\nexit 1\n"
	if err := os.WriteFile(badGo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	knownGoBinsFn = func() []string { return []string{badGo} }

	best, err := findBestGo(defaultGoVersion)
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	if best != "" {
		t.Fatalf("expected empty, got %q", best)
	}
}

func TestFindBestGoVersionUnparseable(t *testing.T) {
	old := knownGoBinsFn
	defer func() { knownGoBinsFn = old }()

	dir := t.TempDir()
	badGo := filepath.Join(dir, "go")
	script := "#!/usr/bin/env bash\necho 'hello world'\nexit 0\n"
	if err := os.WriteFile(badGo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	knownGoBinsFn = func() []string { return []string{badGo} }

	best, err := findBestGo(defaultGoVersion)
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	if best != "" {
		t.Fatalf("expected empty, got %q", best)
	}
}

func TestFindBestGoNoCandidates(t *testing.T) {
	old := knownGoBinsFn
	defer func() { knownGoBinsFn = old }()

	knownGoBinsFn = func() []string { return nil }
	best, err := findBestGo(defaultGoVersion)
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	if best != "" {
		t.Fatalf("expected empty, got %q", best)
	}
}

func TestResolveGoEnsureGoNil(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	got, err := resolveGo(context.Background(), t.TempDir(), nil)
	if err != nil || got != "" {
		t.Fatalf("resolveGo = %q, err = %v", got, err)
	}
}

func TestResolveGoEnsureGoError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	_, err := resolveGo(context.Background(), t.TempDir(), func(ctx context.Context) (string, error) {
		return "", errors.New("ensure failed")
	})
	if err == nil || !strings.Contains(err.Error(), "ensure failed") {
		t.Fatalf("expected ensure error, got %v", err)
	}
}

func TestResolveGoEnsureGoInstalls(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	got, err := resolveGo(context.Background(), t.TempDir(), func(ctx context.Context) (string, error) {
		return "/usr/local/go/bin/go", nil
	})
	if err != nil || got != "/usr/local/go/bin/go" {
		t.Fatalf("resolveGo = %q, err = %v", got, err)
	}
}

func TestResolveGoFindBestGoError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", errors.New("find failed") }
	defer func() { findBestGoFn = old }()

	_, err := resolveGo(context.Background(), t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "find failed") {
		t.Fatalf("expected find error, got %v", err)
	}
}

func TestGoBuildEnvAddsPathWhenMissing(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("Path", "")
	os.Unsetenv("PATH")
	os.Unsetenv("Path")
	env := goBuildEnv("/usr/local/go/bin/go")
	found := false
	for _, e := range env {
		if e == "PATH=:/usr/local/go/bin" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PATH not added: %v", env)
	}
}

func TestGoToolchainEnsureMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache-file")
	if err := os.WriteFile(cacheFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gt := NewGoToolchain(cacheFile)
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

func TestGoToolchainEnsureTmpDirMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	goDir := filepath.Join(dir, "go"+defaultGoVersion)
	tmpDir := filepath.Join(filepath.Dir(goDir), ".tmp-"+defaultGoVersion)
	if err := os.WriteFile(tmpDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gt := NewGoToolchain(dir)
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected tmp dir MkdirAll error")
	}
}

func TestGoToolchainEnsureDownloadCopyError(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	body := []byte("partial tarball data")
	sum := sha256.Sum256(body)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(&errorReader{err: errors.New("read failed")}),
			Request:    req,
		}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "download Go") {
		t.Fatalf("expected download error, got %v", err)
	}
}

func TestGoToolchainEnsureTarNextError(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("not a tar archive"))
	gz.Close()
	tarball := buf.Bytes()
	sum := sha256.Sum256(tarball)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tar Go") {
		t.Fatalf("expected tar error, got %v", err)
	}
}

func buildGoTarballWithHeaders(t *testing.T, entries []struct {
	hdr  tar.Header
	data []byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := e.hdr
		h.Size = int64(len(e.data))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if len(e.data) > 0 {
			if _, err := tw.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGoToolchainEnsureRootDirEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	tarball := buildGoTarballWithHeaders(t, []struct {
		hdr  tar.Header
		data []byte
	}{
		{hdr: tar.Header{Name: "go/", Mode: 0o755, Typeflag: tar.TypeDir}},
		{hdr: tar.Header{Name: "go/bin/go", Mode: 0o755, Typeflag: tar.TypeReg}, data: []byte("#!/bin/sh\necho go version go1.26.4 linux/amd64\n")},
	})
	sum := sha256.Sum256(tarball)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	goBin, err := gt.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := os.Stat(goBin); err != nil {
		t.Fatalf("go binary not found: %v", err)
	}
}

func TestGoToolchainEnsureDirMkdirError(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	goDir := filepath.Join(dir, "go"+defaultGoVersion)
	// Pre-create a file where the tarball wants to create a nested directory.
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "pkg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tarball := buildGoTarballWithHeaders(t, []struct {
		hdr  tar.Header
		data []byte
	}{
		{hdr: tar.Header{Name: "go/pkg/", Mode: 0o755, Typeflag: tar.TypeDir}},
	})
	sum := sha256.Sum256(tarball)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected dir MkdirAll error")
	}
}

func TestGoToolchainEnsureFileOpenError(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	goDir := filepath.Join(dir, "go"+defaultGoVersion)
	targetFile := filepath.Join(goDir, "file.txt")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetFile, 0o755); err != nil {
		t.Fatal(err)
	}

	tarball := buildGoTarballWithHeaders(t, []struct {
		hdr  tar.Header
		data []byte
	}{
		{hdr: tar.Header{Name: "go/file.txt", Mode: 0o644, Typeflag: tar.TypeReg}, data: []byte("x")},
	})
	sum := sha256.Sum256(tarball)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected OpenFile error")
	}
}

func buildTruncatedGoTarball(t *testing.T, fileSize int, written []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "go/file.txt", Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(fileSize)}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(written); err != nil {
		t.Fatal(err)
	}
	// Close only the gzip stream so the tar archive ends mid-file.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGoToolchainEnsureFileCopyError(t *testing.T) {
	dir := t.TempDir()
	key := runtime.GOOS + "-" + runtime.GOARCH
	oldSHA := defaultGoSHA256[key]
	defer func() { defaultGoSHA256[key] = oldSHA }()

	tarball := buildTruncatedGoTarball(t, 10, []byte("12345"))
	sum := sha256.Sum256(tarball)
	defaultGoSHA256[key] = hex.EncodeToString(sum[:])

	gt := NewGoToolchain(dir)
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(tarball)), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected copy error")
	}
}
