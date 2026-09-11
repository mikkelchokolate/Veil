package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestInboundAttachedClientsAreNotLimitedByGlobalListPage(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	inbound := v1Request(t, r, http.MethodPost, "/api/inbounds",
		`{"name":"bound","protocol":"hysteria2","transport":"udp","port":18444,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}

	oldest, err := st.clientRepo.Create(client.Client{Name: "bound-client", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.clientRepo.CreateBinding(client.Binding{ClientID: oldest.ID, InboundID: "bound", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE clients SET created_at=1 WHERE id=?`, oldest.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		row, err := st.clientRepo.Create(client.Client{Name: fmt.Sprintf("other-%02d", i), Enabled: true, QuotaResetPolicy: client.ResetNever})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`UPDATE clients SET created_at=? WHERE id=?`, int64(100+i), row.ID); err != nil {
			t.Fatal(err)
		}
	}

	global := v1Request(t, r, http.MethodGet, "/api/v1/clients?pageSize=25", "")
	var globalResp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(global.Body).Decode(&globalResp); err != nil {
		t.Fatal(err)
	}
	if globalResp.Total != 26 || len(globalResp.Items) != 25 {
		t.Fatalf("global list total=%d len=%d", globalResp.Total, len(globalResp.Items))
	}
	for _, item := range globalResp.Items {
		if item["id"] == oldest.ID {
			t.Fatal("oldest attached client appeared on the global first page")
		}
	}

	attached := v1Request(t, r, http.MethodGet, "/api/inbounds/bound/clients?pageSize=25", "")
	if attached.Code != http.StatusOK {
		t.Fatalf("attached clients: %d %s", attached.Code, attached.Body.String())
	}
	var assoc struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(attached.Body).Decode(&assoc); err != nil {
		t.Fatal(err)
	}
	if assoc.Total != 1 || len(assoc.Items) != 1 || assoc.Items[0]["id"] != oldest.ID {
		t.Fatalf("attached clients=%+v", assoc)
	}
}
