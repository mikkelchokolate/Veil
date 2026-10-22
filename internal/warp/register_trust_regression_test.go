package warp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #1292 F1: the registration response is persisted straight into the WARP
// egress config, so its peer endpoint must not be able to repoint the
// tunnel at an arbitrary host, and its key/address fields must be
// well-formed before they are stored.

const validPeerKey = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="

func registerWithResponse(t *testing.T, body string) (Registration, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
}

func registrationBody(clientID, v4, v6, peerKey, endpointHost string) string {
	return fmt.Sprintf(`{"id":"device-123","token":"tok","account":{"license":"L"},"config":{"client_id":%q,"interface":{"addresses":{"v4":%q,"v6":%q}},"peers":[{"public_key":%q,"endpoint":{"host":%q}}]}}`,
		clientID, v4, v6, peerKey, endpointHost)
}

func TestRegistrarEndpointIsAdvisory(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"engage.cloudflareclient.com:2408", "engage.cloudflareclient.com:2408"},
		{"ENGAGE.CloudflareClient.com:2408", "engage.cloudflareclient.com:2408"},
		{"engage-2.edge.cloudflareclient.com:500", "engage-2.edge.cloudflareclient.com:500"},
		{"cloudflareclient.com:2408", "cloudflareclient.com:2408"},
		{"", DefaultEndpoint},
		{"attacker.example:2408", DefaultEndpoint},
		{"cloudflareclient.com.attacker.example:2408", DefaultEndpoint},
		{"evilcloudflareclient.com:2408", DefaultEndpoint},
		{"203.0.113.7:2408", DefaultEndpoint},
		{"[2001:db8::1]:2408", DefaultEndpoint},
		{"engage.cloudflareclient.com", DefaultEndpoint},
		{"engage.cloudflareclient.com:0", DefaultEndpoint},
		{"engage.cloudflareclient.com:70000", DefaultEndpoint},
		{"engage.cloudflareclient.com:http", DefaultEndpoint},
		{"bad_label.cloudflareclient.com:2408", DefaultEndpoint},
		{"-x.cloudflareclient.com:2408", DefaultEndpoint},
		{"..cloudflareclient.com:2408", DefaultEndpoint},
		{"a\nb.cloudflareclient.com:2408", DefaultEndpoint},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			reg, err := registerWithResponse(t, registrationBody("t0o+", "172.16.0.2", "", validPeerKey, tc.host))
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			if reg.Endpoint != tc.want {
				t.Fatalf("endpoint for registrar host %q = %q, want %q", tc.host, reg.Endpoint, tc.want)
			}
			if err := Validate(Config{Endpoint: reg.Endpoint, SocksPort: 40000, MTU: 1280}); err != nil {
				t.Fatalf("accepted endpoint %q must pass Validate: %v", reg.Endpoint, err)
			}
		})
	}
}

func TestRegistrarRejectsMalformedRegistrationFields(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantError string
	}{
		{"peer key not base64", registrationBody("t0o+", "172.16.0.2", "", "not base64!", "engage.cloudflareclient.com:2408"), "peer public_key"},
		{"peer key short", registrationBody("t0o+", "172.16.0.2", "", "PEERPUBKEY=", "engage.cloudflareclient.com:2408"), "peer public_key"},
		{"peer key long", registrationBody("t0o+", "172.16.0.2", "", validPeerKey[:43]+"AAAA", "engage.cloudflareclient.com:2408"), "peer public_key"},
		{"v4 garbage", registrationBody("t0o+", "172.16.0.2\nAllowedIPs", "", validPeerKey, ""), "v4 address"},
		{"v4 with prefix", registrationBody("t0o+", "172.16.0.2/24", "", validPeerKey, ""), "v4 address"},
		{"v4 is v6", registrationBody("t0o+", "2606:4700::1", "", validPeerKey, ""), "v4 address"},
		{"v4 unspecified", registrationBody("t0o+", "0.0.0.0", "", validPeerKey, ""), "v4 address"},
		{"v4 loopback", registrationBody("t0o+", "127.0.0.1", "", validPeerKey, ""), "v4 address"},
		{"v6 is v4", registrationBody("t0o+", "", "172.16.0.2", validPeerKey, ""), "v6 address"},
		{"v6 mapped v4", registrationBody("t0o+", "", "::ffff:172.16.0.2", validPeerKey, ""), "v6 address"},
		{"v6 zoned", registrationBody("t0o+", "", "fe80::1%eth0", validPeerKey, ""), "v6 address"},
		{"v6 multicast", registrationBody("t0o+", "", "ff02::1", validPeerKey, ""), "v6 address"},
		{"reserved too long", registrationBody("AQIDBA==", "172.16.0.2", "", validPeerKey, ""), "3 reserved bytes"},
		{"reserved too short", registrationBody("AQI=", "172.16.0.2", "", validPeerKey, ""), "3 reserved bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, err := registerWithResponse(t, tc.body)
			if err == nil {
				t.Fatalf("malformed registration accepted: %+v", reg)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.wantError)
			}
		})
	}
}

func TestRegistrarAcceptsWellFormedDualStackRegistration(t *testing.T) {
	reg, err := registerWithResponse(t, registrationBody("t0o+", "172.16.0.2", "2606:4700:110:8cdc::1", validPeerKey, "engage.cloudflareclient.com:2408"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.PeerPublicKey != validPeerKey || reg.LocalAddress != "172.16.0.2/32,2606:4700:110:8cdc::1/128" || len(reg.Reserved) != 3 {
		t.Fatalf("registration = %+v", reg)
	}
	// An absent client_id still yields no reserved bytes, as before.
	reg, err = registerWithResponse(t, registrationBody("", "172.16.0.2", "", validPeerKey, ""))
	if err != nil || len(reg.Reserved) != 0 || reg.Endpoint != DefaultEndpoint {
		t.Fatalf("registration without client_id = %+v, err=%v", reg, err)
	}
}
