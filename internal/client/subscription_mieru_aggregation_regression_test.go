package client

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
)

func TestLinksForSnapshotAggregatesMieruTCPAndUDP(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)
	r := NewSubscriptionRenderer(repo, cs).WithSettings(clientaccess.Settings{Domain: "vpn.example.com"})

	c, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "mieru-tcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	udp, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "mieru-udp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "mieru-disabled", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Set(tcp.ID, "password", "alice-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Set(udp.ID, "password", "alice-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Set(disabled.ID, "password", "alice-pass"); err != nil {
		t.Fatal(err)
	}

	resolve := func(inboundID string) (InboundSnapshot, bool) {
		switch inboundID {
		case "mieru-tcp":
			return InboundSnapshot{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}, true
		case "mieru-udp":
			return InboundSnapshot{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true}, true
		case "mieru-disabled":
			return InboundSnapshot{Name: "mieru-disabled", Protocol: "mieru", Transport: "tcp", Port: 8443, Enabled: true}, true
		default:
			return InboundSnapshot{}, false
		}
	}
	links, err := r.LinksForClient(c, resolve)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 aggregated Mieru link, got %d (%+v)", len(links), links)
	}
	var config struct {
		Profiles []struct {
			Servers []struct {
				PortBindings []struct {
					Port     int    `json:"port"`
					Protocol string `json:"protocol"`
				} `json:"portBindings"`
			} `json:"servers"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(links[0].Config), &config); err != nil {
		t.Fatalf("config: %v\n%s", err, links[0].Config)
	}
	if len(config.Profiles) != 1 || len(config.Profiles[0].Servers) != 1 || len(config.Profiles[0].Servers[0].PortBindings) != 2 {
		t.Fatalf("portBindings = %+v, want TCP+UDP", config)
	}
	gotTCP, gotUDP := false, false
	for _, binding := range config.Profiles[0].Servers[0].PortBindings {
		if binding.Protocol == "TCP" {
			gotTCP = true
		}
		if binding.Protocol == "UDP" {
			gotUDP = true
		}
	}
	if !gotTCP || !gotUDP {
		t.Fatalf("aggregated config missing TCP/UDP: %+v", config.Profiles[0].Servers[0].PortBindings)
	}
	if strings.Count(links[0].URI, "port=443") != 2 || !strings.Contains(links[0].URI, "protocol=TCP") || !strings.Contains(links[0].URI, "protocol=UDP") {
		t.Fatalf("aggregated URI missing both bindings: %q", links[0].URI)
	}
	if strings.Contains(links[0].Config, "8443") {
		t.Fatalf("disabled binding leaked into aggregated config: %s", links[0].Config)
	}
}

func TestLinksForSnapshotSingleMieruInboundStaysSingleBinding(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)
	r := NewSubscriptionRenderer(repo, cs).WithSettings(clientaccess.Settings{Domain: "vpn.example.com"})

	c, _ := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "mieru-tcp", Enabled: true})
	_, _ = cs.Set(b.ID, "password", "bob-pass")

	links, err := r.LinksForClient(c, func(inboundID string) (InboundSnapshot, bool) {
		return InboundSnapshot{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 2999, Enabled: true}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("links=%d, want 1", len(links))
	}
	if strings.Count(links[0].URI, "port=2999") != 1 {
		t.Fatalf("single inbound URI = %q", links[0].URI)
	}
}
