package api

import "testing"

func TestRouteDatResponsePolicyClassifiesStatuses(t *testing.T) {
	policy := NewRouteDatResponsePolicy()
	if decision := policy.Decide("https://example.com/geosite.dat", 200, "200 OK"); !decision.Accept || decision.Retry || decision.Err != nil {
		t.Fatalf("200 decision = %+v", decision)
	}
	if decision := policy.Decide("https://example.com/geosite.dat", 503, "503 Service Unavailable"); decision.Accept || !decision.Retry || decision.Err == nil || decision.Err.Error() != "download https://example.com/geosite.dat returned 503 Service Unavailable" {
		t.Fatalf("503 decision = %+v", decision)
	}
	if decision := policy.Decide("https://example.com/geosite.dat", 404, "404 Not Found"); decision.Accept || decision.Retry || decision.Err == nil || decision.Err.Error() != "download https://example.com/geosite.dat returned 404 Not Found" {
		t.Fatalf("404 decision = %+v", decision)
	}
}
