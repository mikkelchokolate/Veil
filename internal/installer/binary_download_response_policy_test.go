package installer

import "testing"

func TestBinaryDownloadResponsePolicyClassifiesStatuses(t *testing.T) {
	policy := NewBinaryDownloadResponsePolicy()
	if decision := policy.Decide(200, "200 OK"); !decision.Accept || decision.Retry || decision.Err != nil {
		t.Fatalf("200 decision = %+v", decision)
	}
	if decision := policy.Decide(503, "503 Service Unavailable"); decision.Accept || !decision.Retry || decision.Err == nil || decision.Err.Error() != "download failed: 503 Service Unavailable" {
		t.Fatalf("503 decision = %+v", decision)
	}
	if decision := policy.Decide(404, "404 Not Found"); decision.Accept || decision.Retry || decision.Err == nil || decision.Err.Error() != "download failed: 404 Not Found" {
		t.Fatalf("404 decision = %+v", decision)
	}
}
