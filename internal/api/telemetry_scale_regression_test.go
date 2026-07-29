package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestTrafficSummaryAggregatesAllTenThousandAndOneClients(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	tx, err := state.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	clientStatement, err := tx.Prepare(`INSERT INTO clients
	  (id,name,enabled,quota_bytes,quota_reset_policy,depleted,created_at,updated_at,version)
	  VALUES(?,?,1,?,'never',0,?,?,1)`)
	if err != nil {
		t.Fatal(err)
	}
	counterStatement, err := tx.Prepare(`INSERT INTO traffic_counters
	  (client_id,binding_id,upload_bytes,download_bytes,last_observed_at,telemetry_state,updated_at)
	  VALUES(?,'',1,2,?,'observed',?)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	quota := int64(1)
	for index := 0; index < 10001; index++ {
		id := fmt.Sprintf("scale-client-%05d", index)
		if _, err := clientStatement.Exec(id, id, quota, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := counterStatement.Exec(id, now, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = clientStatement.Close()
	_ = counterStatement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	response := v1Request(t, router, http.MethodGet, "/api/v1/traffic/summary", "")
	if response.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := int64(body["uploadBytes"].(float64)); got != 10001 {
		t.Errorf("uploadBytes=%d want=10001", got)
	}
	if got := int64(body["downloadBytes"].(float64)); got != 20002 {
		t.Errorf("downloadBytes=%d want=20002", got)
	}
}

func TestClientListReportsServerEnforcedPageMaximum(t *testing.T) {
	router, _ := newApplyTrackedRouterWithState(t)
	response := v1Request(t, router, http.MethodGet, "/api/v1/clients?page=1&pageSize=1000000", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := int(body["pageSize"].(float64)); got != 100 {
		t.Fatalf("response pageSize=%d want enforced maximum 100", got)
	}
}
