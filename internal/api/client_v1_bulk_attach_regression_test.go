package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestV1BulkAttachInboundIssuesCredentialAndApplySucceeds(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	inbound := v1Request(t, r, http.MethodPost, "/api/inbounds",
		`{"name":"hy-bulk","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true,"protocolFields":{"domain":"hy.example.com"}}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"bulk-attach"}`).Body.Bytes())
	id := created["id"].(string)

	w := v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk",
		`{"action":"attach_inbound","inboundId":"hy-bulk","clientIds":["`+id+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk attach: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Succeeded int  `json:"succeeded"`
		Failed    int  `json:"failed"`
		Success   bool `json:"success"`
		Results   []struct {
			ID                string `json:"id"`
			OK                bool   `json:"ok"`
			BindingID         string `json:"bindingId"`
			Plaintext         string `json:"plaintext"`
			IssuedCredentials []struct {
				Plaintext string `json:"plaintext"`
			} `json:"issuedCredentials"`
		} `json:"results"`
		ApplyJob *struct {
			Status string `json:"status"`
		} `json:"applyJob"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Succeeded != 1 || resp.Failed != 0 || !resp.Results[0].OK {
		t.Fatalf("bulk attach accounting=%+v", resp)
	}
	if resp.Results[0].Plaintext == "" || resp.Results[0].BindingID == "" {
		t.Fatalf("missing issued credential: %+v", resp.Results[0])
	}
	if resp.ApplyJob == nil {
		t.Fatalf("bulk attach missing apply job: %+v", resp)
	}
	if !resp.Success {
		jobJSON, _ := json.Marshal(resp.ApplyJob)
		if strings.Contains(strings.ToLower(string(jobJSON)), "no active credential") {
			t.Fatalf("bulk attach still produced an enabled binding without credentials: %s", jobJSON)
		}
	}

	view := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id, "")
	var got map[string]any
	if err := json.NewDecoder(view.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["hasCredentials"] != true {
		t.Fatalf("hasCredentials=%v", got["hasCredentials"])
	}
	creds, err := st.clientService.CredentialsForInbound("hy-bulk")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Password == "" {
		t.Fatalf("runtime credentials=%+v", creds)
	}
}
