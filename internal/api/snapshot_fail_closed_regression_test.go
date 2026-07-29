package api

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type snapshotTreeEntry struct {
	Mode fs.FileMode
	Body []byte
}

func captureSnapshotTestTree(t *testing.T, root string) map[string]snapshotTreeEntry {
	t.Helper()
	entries := make(map[string]snapshotTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := snapshotTreeEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			item.Body, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		entries[rel] = item
		return nil
	})
	if err != nil {
		t.Fatalf("capture %s: %v", root, err)
	}
	return entries
}

func snapshotTestTableCount(t *testing.T, st *managementState, table string) int {
	t.Helper()
	allowed := map[string]bool{"revision_snapshots": true, "apply_jobs": true}
	if !allowed[table] {
		t.Fatalf("test attempted to count unexpected table %q", table)
	}
	var count int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestImmutableSnapshotReadFailuresAbortManagementMutation(t *testing.T) {
	faults := []struct {
		name  string
		table string
	}{
		{name: "clients", table: "clients"},
		{name: "bindings", table: "client_bindings"},
		{name: "credentials", table: "client_credentials"},
	}

	for index, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			router, st := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = st.Close() })

			if err := os.MkdirAll(st.liveRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(st.liveRoot, "runtime-sentinel")
			if err := os.WriteFile(sentinel, []byte("unchanged-live-runtime"), 0o600); err != nil {
				t.Fatal(err)
			}

			stateBefore, err := os.ReadFile(st.statePath)
			if err != nil {
				t.Fatal(err)
			}
			liveBefore := captureSnapshotTestTree(t, st.liveRoot)
			revisionsBefore, err := st.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			snapshotsBefore := snapshotTestTableCount(t, st, "revision_snapshots")
			jobsBefore := snapshotTestTableCount(t, st, "apply_jobs")

			faultedTable := fault.table + "_snapshot_fault"
			if _, err := st.db.Exec("ALTER TABLE " + fault.table + " RENAME TO " + faultedTable); err != nil {
				t.Fatalf("inject %s snapshot read fault: %v", fault.name, err)
			}

			body := fmt.Sprintf(`{"name":"snapshot-fault-%d","protocol":"hysteria2","transport":"udp","port":%d,"enabled":true}`, index, 15443+index)
			response := postJSON(t, router, "/api/inbounds", body)
			if response.Code >= http.StatusOK && response.Code < http.StatusMultipleChoices {
				t.Errorf("mutation unexpectedly succeeded with %s snapshot read fault: %d %s", fault.name, response.Code, response.Body.String())
			}

			stateAfter, err := os.ReadFile(st.statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateAfter, stateBefore) {
				t.Errorf("state.json changed after %s snapshot read fault", fault.name)
			}
			revisionsAfter, err := st.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			if revisionsAfter.Desired != revisionsBefore.Desired {
				t.Errorf("desired revision changed after %s fault: %d -> %d", fault.name, revisionsBefore.Desired, revisionsAfter.Desired)
			}
			if got := snapshotTestTableCount(t, st, "revision_snapshots"); got != snapshotsBefore {
				t.Errorf("revision snapshot count changed after %s fault: %d -> %d", fault.name, snapshotsBefore, got)
			}
			if got := snapshotTestTableCount(t, st, "apply_jobs"); got != jobsBefore {
				t.Errorf("apply job count changed after %s fault: %d -> %d", fault.name, jobsBefore, got)
			}
			liveAfter := captureSnapshotTestTree(t, st.liveRoot)
			if !reflect.DeepEqual(liveAfter, liveBefore) {
				beforeNames := make([]string, 0, len(liveBefore))
				for name := range liveBefore {
					beforeNames = append(beforeNames, name)
				}
				afterNames := make([]string, 0, len(liveAfter))
				for name := range liveAfter {
					afterNames = append(afterNames, name)
				}
				sort.Strings(beforeNames)
				sort.Strings(afterNames)
				t.Errorf("live runtime changed after %s fault: before=%v after=%v", fault.name, beforeNames, afterNames)
			}
		})
	}
}
