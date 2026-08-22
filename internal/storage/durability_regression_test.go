package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const sqliteKillHelperEnv = "VEIL_SQLITE_KILL_HELPER"

func TestCriticalSQLiteConnectionsUseFullSynchronous(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 4; i++ {
		var synchronous int
		if err := db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		// SQLite: 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.
		if synchronous < 2 {
			t.Fatalf("critical SQLite connection synchronous=%d; require FULL(2) or EXTRA(3)", synchronous)
		}
	}
}

func TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill(t *testing.T) {
	if os.Getenv(sqliteKillHelperEnv) == "1" {
		runSQLiteKillHelper()
		return
	}
	root := t.TempDir()
	databasePath := filepath.Join(root, "veil.db")
	marker := filepath.Join(root, "committed")
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill$")
	command.Env = append(os.Environ(),
		sqliteKillHelperEnv+"=1",
		"VEIL_SQLITE_KILL_DATABASE="+databasePath,
		"VEIL_SQLITE_KILL_MARKER="+marker,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatal("child did not return from durable commit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = command.Process.Wait()

	db, err := OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var desired uint64
	if err := db.QueryRow(`SELECT desired_revision FROM revisions WHERE id=1`).Scan(&desired); err != nil {
		t.Fatal(err)
	}
	if desired != 1 {
		t.Fatalf("desired revision after process kill=%d want=1", desired)
	}
	var payload string
	if err := db.QueryRow(`SELECT payload FROM revision_snapshots WHERE revision=1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"durable":true}` {
		t.Fatalf("snapshot after process kill=%q", payload)
	}
}

func runSQLiteKillHelper() {
	databasePath := os.Getenv("VEIL_SQLITE_KILL_DATABASE")
	marker := os.Getenv("VEIL_SQLITE_KILL_MARKER")
	db, err := Open(databasePath)
	if err != nil {
		os.Exit(72)
	}
	tx, err := db.Begin()
	if err != nil {
		os.Exit(73)
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 0, 0)`); err != nil {
		os.Exit(74)
	}
	if _, err = tx.Exec(`UPDATE revisions SET desired_revision=1 WHERE id=1`); err != nil {
		os.Exit(75)
	}
	if _, err = tx.Exec(`INSERT INTO revision_snapshots(revision, payload, created_at, state_sha256) VALUES(1, ?, 1, ?)`, `{"durable":true}`, "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
		os.Exit(76)
	}
	if err = tx.Commit(); err != nil {
		os.Exit(77)
	}
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		os.Exit(78)
	}
	_, _ = file.WriteString("committed")
	_ = file.Sync()
	_ = file.Close()
	select {}
}
