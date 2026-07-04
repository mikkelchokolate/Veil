package inbounds

import "testing"

func TestInboundCatalogGet(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator([]Inbound{{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
	}}, func() string { return "p" })

	if got, ok := catalog.Get("naive"); !ok || got.Name != "naive" {
		t.Fatalf("expected to find naive, got %+v ok=%v", got, ok)
	}
	if _, ok := catalog.Get("missing"); ok {
		t.Fatalf("expected not to find missing")
	}
}

func TestInboundCatalogDelete(t *testing.T) {
	cases := []struct {
		name        string
		inbounds    []Inbound
		delete      string
		wantErr     error
		wantMissing bool
	}{
		{
			name: "deletes existing inbound",
			inbounds: []Inbound{{
				Name:      "naive",
				Protocol:  "naiveproxy",
				Transport: "tcp",
				Port:      443,
			}},
			delete:      "naive",
			wantErr:     nil,
			wantMissing: true,
		},
		{
			name: "returns error when inbound not found",
			inbounds: []Inbound{{
				Name:      "naive",
				Protocol:  "naiveproxy",
				Transport: "tcp",
				Port:      443,
			}},
			delete:      "missing",
			wantErr:     ErrInboundNotFound,
			wantMissing: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := NewInboundCatalogWithPasswordGenerator(tc.inbounds, func() string { return "p" })
			next, err := catalog.Delete(tc.delete)
			if err != tc.wantErr {
				t.Fatalf("Delete error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && len(next.List()) != len(tc.inbounds) {
				t.Fatalf("catalog changed on error: got %d inbounds", len(next.List()))
			}
			if _, ok := next.Get(tc.delete); tc.wantMissing && ok {
				t.Fatalf("expected %q to be deleted", tc.delete)
			}
		})
	}
}

func TestInboundCatalogCreateErrors(t *testing.T) {
	base := NewInboundCatalogWithPasswordGenerator([]Inbound{{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
	}}, func() string { return "p" })

	cases := []struct {
		name    string
		inbound Inbound
		wantErr error
	}{
		{
			name:    "rejects invalid inbound",
			inbound: Inbound{Name: "x", Protocol: "naiveproxy", Transport: "tcp"},
			wantErr: ErrInboundInvalid,
		},
		{
			name:    "rejects unsupported protocol/transport",
			inbound: Inbound{Name: "x", Protocol: "naiveproxy", Transport: "udp", Port: 443},
			wantErr: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:    "rejects duplicate name",
			inbound: Inbound{Name: "naive", Protocol: "mieru", Transport: "udp", Port: 8443},
			wantErr: ErrInboundDuplicateName,
		},
		{
			name:    "rejects duplicate transport/port",
			inbound: Inbound{Name: "naive-alt", Protocol: "mieru", Transport: "tcp", Port: 443},
			wantErr: ErrInboundDuplicateTransportPort,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created, next, err := base.Create(tc.inbound)
			if err != tc.wantErr {
				t.Fatalf("Create error = %v, want %v", err, tc.wantErr)
			}
			if created.Name != "" {
				t.Fatalf("created inbound should be zero, got %+v", created)
			}
			if tc.wantErr == ErrInboundDuplicateName {
				if got, ok := next.Get("naive"); !ok || got.Protocol != "naiveproxy" {
					t.Fatalf("original inbound changed: %+v", got)
				}
			} else if tc.wantErr != nil {
				if _, ok := next.Get(tc.inbound.Name); ok {
					t.Fatalf("inbound %q should not have been created", tc.inbound.Name)
				}
			}
		})
	}
}

func TestInboundCatalogUpdateErrors(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator([]Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443},
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 444},
	}, func() string { return "p" })

	cases := []struct {
		name    string
		target  string
		update  Inbound
		wantErr error
	}{
		{
			name:    "rejects update when inbound not found",
			target:  "missing",
			update:  Inbound{Protocol: "naiveproxy", Transport: "tcp", Port: 443},
			wantErr: ErrInboundNotFound,
		},
		{
			name:    "rejects invalid update",
			target:  "naive",
			update:  Inbound{Protocol: "naiveproxy", Transport: "tcp"},
			wantErr: ErrInboundInvalid,
		},
		{
			name:    "rejects unsupported protocol/transport",
			target:  "naive",
			update:  Inbound{Protocol: "naiveproxy", Transport: "udp", Port: 443},
			wantErr: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:    "rejects duplicate transport/port from another inbound",
			target:  "mieru",
			update:  Inbound{Protocol: "mieru", Transport: "tcp", Port: 443},
			wantErr: ErrInboundDuplicateTransportPort,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := catalog.Update(tc.target, tc.update)
			if err != tc.wantErr {
				t.Fatalf("Update error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestInboundCatalogUpdateKeepsSameTransportPort(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator([]Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443},
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 444},
	}, func() string { return "p" })

	updated, _, err := catalog.Update("naive", Inbound{Protocol: "naiveproxy", Transport: "tcp", Port: 443})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Port != 443 {
		t.Fatalf("port = %d, want 443", updated.Port)
	}
}

func TestInboundCatalogDefaultPasswordGenerator(t *testing.T) {
	catalog := NewCatalog(nil)
	created, next, err := catalog.Create(Inbound{
		Name:      "hy2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password == "" {
		t.Fatal("expected generated password")
	}
	got, ok := next.Get("hy2")
	if !ok || got.Password == "" {
		t.Fatalf("stored password missing: %+v ok=%v", got, ok)
	}
}

func TestInboundCatalogWithNilPasswordGenerator(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator(nil, nil)
	created, _, err := catalog.Create(Inbound{
		Name:      "mieru",
		Protocol:  "mieru",
		Transport: "tcp",
		Port:      443,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password == "" {
		t.Fatal("expected generated password when generator is nil")
	}
}
