package api

import "testing"

func TestParseSpeedtestCLIJSONConvertsBitsPerSecondToMbps(t *testing.T) {
	result, err := parseSpeedtestCLIJSON([]byte(`{
		"ping": 11.2,
		"download": 104000000,
		"upload": 52000000,
		"server": {"sponsor":"Test ISP", "name":"Moscow"}
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PingMS != 11.2 || result.DownloadMbps != 104 || result.UploadMbps != 52 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Server != "Test ISP - Moscow" {
		t.Fatalf("unexpected server: %q", result.Server)
	}
}

func TestParseSpeedtestCLIJSONServerLabelNoDanglingDelimiters(t *testing.T) {
	tests := []struct {
		name    string
		sponsor string
		srvName string
		want    string
	}{
		{"both present", "Test ISP", "Moscow", "Test ISP - Moscow"},
		{"sponsor missing", "", "Moscow", "Moscow"},
		{"name missing", "Test ISP", "", "Test ISP"},
		{"both missing", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(`{"ping":1,"download":1,"upload":1,"server":{"sponsor":"` + tt.sponsor + `","name":"` + tt.srvName + `"}}`)
			result, err := parseSpeedtestCLIJSON(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Server != tt.want {
				t.Fatalf("server = %q, want %q", result.Server, tt.want)
			}
		})
	}
}

func TestParseOoklaSpeedtestJSONConvertsBytesPerSecondToMbps(t *testing.T) {
	result, err := parseOoklaSpeedtestJSON([]byte(`{
		"ping": {"latency": 9.5},
		"download": {"bandwidth": 12500000},
		"upload": {"bandwidth": 6250000},
		"server": {"name":"Moscow"},
		"isp":"Test ISP"
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PingMS != 9.5 || result.DownloadMbps != 100 || result.UploadMbps != 50 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Server != "Test ISP - Moscow" {
		t.Fatalf("unexpected server: %q", result.Server)
	}
}
