package acmeip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIssuanceFailureMustPreserveExistingACMEState(t *testing.T) {
	root := t.TempDir()
	sys := newFakeSystem()
	sys.home = root
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	ip := "192.0.2.20"
	eccDir := filepath.Join(root, ".acme.sh", ip+"_ecc")
	if err := os.MkdirAll(eccDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(eccDir, ip+".key")
	confPath := filepath.Join(eccDir, ip+".conf")
	keyBytes := []byte("preexisting-acme-key")
	confBytes := []byte("preexisting-renewal-conf")
	if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(confPath, confBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	sys.files[eccDir] = &fakeFileInfo{name: ip + "_ecc", mode: os.ModeDir | 0o700}
	sys.files[keyPath] = &fakeFileInfo{name: ip + ".key", mode: 0o600}
	sys.files[confPath] = &fakeFileInfo{name: ip + ".conf", mode: 0o600}

	wrapper := &fsCleanupSystem{fakeSystem: sys, root: root}
	acmeSh := filepath.Join(root, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", ip, "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{err: errors.New("validation failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: ip, System: wrapper})
	if err == nil {
		t.Fatal("expected issuance error")
	}
	gotKey, readErr := os.ReadFile(keyPath)
	if readErr != nil {
		t.Fatalf("preexisting ACME key was deleted: %v", readErr)
	}
	gotConf, readErr := os.ReadFile(confPath)
	if readErr != nil {
		t.Fatalf("preexisting ACME renewal config was deleted: %v", readErr)
	}
	if string(gotKey) != string(keyBytes) || string(gotConf) != string(confBytes) {
		t.Fatalf("preexisting ACME state was modified: key=%q conf=%q", gotKey, gotConf)
	}
	for _, call := range sys.runCalls {
		if len(call) >= 3 && call[0] == "rm" {
			for _, arg := range call[2:] {
				if arg == eccDir {
					t.Fatalf("cleanup removed preexisting ACME directory: %v", call)
				}
			}
		}
		if len(call) >= 3 && call[0] == acmeSh && call[1] == "--remove" {
			t.Fatalf("cleanup ran acme.sh --remove against preexisting state: %v", call)
		}
	}
}

func TestFirstTimeIssuanceFailureStillCleansOwnedState(t *testing.T) {
	root := t.TempDir()
	sys := newFakeSystem()
	sys.home = root
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	ip := "192.0.2.21"
	wrapper := &fsCleanupSystem{fakeSystem: sys, root: root}
	acmeSh := filepath.Join(root, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", ip, "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{
		err: errors.New("validation failed"),
		writeOwned: func() {
			dir := filepath.Join(root, ".acme.sh", ip+"_ecc")
			_ = os.MkdirAll(dir, 0o700)
			_ = os.WriteFile(filepath.Join(dir, "tmp"), []byte("owned"), 0o600)
		},
	}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: ip, System: wrapper})
	if err == nil {
		t.Fatal("expected issuance error")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".acme.sh", ip+"_ecc")); !os.IsNotExist(statErr) {
		t.Fatalf("first-time failed issuance left owned ACME directory: %v", statErr)
	}
}

type fsCleanupSystem struct {
	*fakeSystem
	root string
}

func (s *fsCleanupSystem) CombinedOutput(cmd string, args ...string) ([]byte, error) {
	key := s.key(cmd, args...)
	if res, ok := s.commands[key]; ok && res.writeOwned != nil {
		res.writeOwned()
	}
	return s.fakeSystem.CombinedOutput(cmd, args...)
}

func (s *fsCleanupSystem) CombinedOutputContext(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.CombinedOutput(cmd, args...)
}

func (s *fsCleanupSystem) Stat(name string) (os.FileInfo, error) {
	if fi, err := os.Stat(name); err == nil {
		return fi, nil
	}
	return s.fakeSystem.Stat(name)
}

func (s *fsCleanupSystem) Run(cmd string, args ...string) error {
	if cmd == "rm" && len(args) > 0 && args[0] == "-rf" {
		s.events = append(s.events, "cleanup")
		s.runCalls = append(s.runCalls, append([]string{cmd}, args...))
		for _, path := range args[1:] {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
		return nil
	}
	return s.fakeSystem.Run(cmd, args...)
}
