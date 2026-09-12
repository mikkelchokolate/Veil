package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExpiredIdempotencyReservationTakeoverIgnoresReusedPID(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	request, scope, fingerprint := pidReuseIdempotencyRequest()

	oldStore := newIdempotencyStore(db)
	oldStore.now = func() time.Time { return now }
	oldStore.owner = fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	owned, oldRecord, err := oldStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned || oldRecord.Generation != 1 {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, oldRecord, err)
	}
	if _, err := db.Exec(`UPDATE idempotency_records SET reserved_until=? WHERE scope=?`, now.Add(-time.Second).Unix(), scope); err != nil {
		t.Fatal(err)
	}

	newStore := newIdempotencyStore(db)
	newStore.now = func() time.Time { return now }
	newStore.owner = fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	owned, newRecord, err := newStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned || newRecord.Generation != 2 {
		t.Fatalf("expired reused-PID reservation was not taken over: owned=%v record=%+v err=%v", owned, newRecord, err)
	}
}

func TestIdempotencySameProcessValidLeaseIsNotTakenOver(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	request, scope, fingerprint := pidReuseIdempotencyRequest()

	store := newIdempotencyStore(db)
	store.now = func() time.Time { return now }
	owned, first, err := store.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, first, err)
	}
	owned, second, err := store.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || owned {
		t.Fatalf("same-process valid lease was taken over: owned=%v record=%+v err=%v", owned, second, err)
	}
	if second.Generation != first.Generation || second.Owner != store.owner {
		t.Fatalf("same-process reservation changed: %+v", second)
	}
}

func TestIdempotencyDeadPIDDoesNotTakeOverBeforeLeaseExpiry(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	request, scope, fingerprint := pidReuseIdempotencyRequest()

	oldStore := newIdempotencyStore(db)
	oldStore.now = func() time.Time { return now }
	oldStore.owner = "pid:999999:" + uuid.NewString()
	owned, first, err := oldStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, first, err)
	}

	newStore := newIdempotencyStore(db)
	newStore.now = func() time.Time { return now }
	owned, current, err := newStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || owned {
		t.Fatalf("dead PID with valid lease was taken over: owned=%v record=%+v err=%v", owned, current, err)
	}
	if current.Generation != 1 {
		t.Fatalf("generation=%d", current.Generation)
	}
}

func TestIdempotencyDeadPIDTakeoverAfterLeaseExpiry(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	request, scope, fingerprint := pidReuseIdempotencyRequest()

	oldStore := newIdempotencyStore(db)
	oldStore.now = func() time.Time { return now }
	oldStore.owner = "pid:999999:" + uuid.NewString()
	owned, first, err := oldStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, first, err)
	}
	if _, err := db.Exec(`UPDATE idempotency_records SET reserved_until=? WHERE scope=?`, now.Add(-time.Second).Unix(), scope); err != nil {
		t.Fatal(err)
	}

	newStore := newIdempotencyStore(db)
	newStore.now = func() time.Time { return now }
	owned, takeover, err := newStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned || takeover.Generation != 2 {
		t.Fatalf("dead PID after lease expiry was not taken over: owned=%v record=%+v err=%v", owned, takeover, err)
	}
}

func TestIdempotencyRecoversCommittedDomainOperationAfterPIDReuse(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	request, scope, fingerprint := pidReuseIdempotencyRequest()

	oldStore := newIdempotencyStore(db)
	oldStore.now = func() time.Time { return now }
	oldStore.owner = fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	owned, first, err := oldStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, first, err)
	}
	if _, err := db.Exec(`UPDATE domain_operations SET state='mutation_committed', domain_result_json=? WHERE id=?`,
		`{"status":"committed","id":"recovered"}`, first.OperationID); err != nil {
		t.Fatal(err)
	}

	newStore := newIdempotencyStore(db)
	newStore.now = func() time.Time { return now }
	newStore.owner = fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	owned, recovered, err := newStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || owned {
		t.Fatalf("committed domain recovery should replay, not re-execute: owned=%v record=%+v err=%v", owned, recovered, err)
	}
	if recovered.State != "completed" || !strings.Contains(string(recovered.Body), "recovered") {
		t.Fatalf("committed domain operation was not recovered: %+v body=%s", recovered, recovered.Body)
	}
}

func TestIdempotencyOwnerAliveUsesNumericPIDOnly(t *testing.T) {
	if !idempotencyOwnerAlive(fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())) {
		t.Fatal("current process PID should be treated as alive")
	}
	if idempotencyOwnerAlive("pid:999999:" + uuid.NewString()) {
		t.Fatal("unused PID should not be treated as alive")
	}
}

func pidReuseIdempotencyRequest() (*http.Request, string, string) {
	request := httptest.NewRequest(http.MethodPost, "/api/clients", strings.NewReader(`{"name":"one"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "same")
	body := []byte(`{"name":"one"}`)
	return request, idempotencyScope(request, "same"), idempotencyFingerprint(request, body)
}
