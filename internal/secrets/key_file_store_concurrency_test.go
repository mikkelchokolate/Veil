package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrCreateKeyConcurrentCallersShareCommittedKey(t *testing.T) {
	path := tempKeyPath(t)
	const callers = 32
	start := make(chan struct{})
	results := make(chan [KeySize]byte, callers)
	errorsCh := make(chan error, callers)
	var wg sync.WaitGroup

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			key, err := LoadOrCreateKey(path)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- *key
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		t.Fatalf("concurrent LoadOrCreateKey: %v", err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != KeySize {
		t.Fatalf("key file length=%d want %d", len(onDisk), KeySize)
	}
	var committed [KeySize]byte
	copy(committed[:], onDisk)
	for result := range results {
		if result != committed {
			t.Fatal("caller returned a key different from the committed key")
		}
	}
}

func TestLoadOrCreateKeyDoesNotPublishBeforeSync(t *testing.T) {
	oldSync := syncKeyFile
	syncErr := errors.New("injected sync failure")
	syncKeyFile = func(*os.File) error { return syncErr }
	defer func() { syncKeyFile = oldSync }()

	path := tempKeyPath(t)
	if _, err := LoadOrCreateKey(path); !errors.Is(err, syncErr) {
		t.Fatalf("error=%v want %v", err, syncErr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("key was published before sync: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state.key.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary key files remain: %v", matches)
	}
}
