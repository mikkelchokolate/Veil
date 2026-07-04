package diagnostics

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

var errSpeedtestTest = errors.New("speedtest test error")

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

func TestParseSpeedtestCLIJSONTrimsWhitespace(t *testing.T) {
	tests := []struct {
		name    string
		sponsor string
		srvName string
		want    string
	}{
		{"sponsor leading space", " Test ISP", "Moscow", "Test ISP - Moscow"},
		{"sponsor trailing space", "Test ISP ", "Moscow", "Test ISP - Moscow"},
		{"name leading space", "Test ISP", " Moscow", "Test ISP - Moscow"},
		{"name trailing space", "Test ISP", "Moscow ", "Test ISP - Moscow"},
		{"both with spaces", "  Test ISP  ", "  Moscow  ", "Test ISP - Moscow"},
		{"sponsor only with spaces", "  Test ISP  ", "", "Test ISP"},
		{"name only with spaces", "", "  Moscow  ", "Moscow"},
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

func TestParseSpeedtestCLIJSONReturnsErrorOnInvalidJSON(t *testing.T) {
	_, err := parseSpeedtestCLIJSON([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseOoklaSpeedtestJSONServerLabelFallback(t *testing.T) {
	tests := []struct {
		name    string
		isp     string
		srvName string
		want    string
	}{
		{"both present", "Test ISP", "Moscow", "Test ISP - Moscow"},
		{"isp missing", "", "Moscow", "Moscow"},
		{"server name missing", "Test ISP", "", "Test ISP"},
		{"both missing", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(`{"ping":{"latency":1},"download":{"bandwidth":1},"upload":{"bandwidth":1},"server":{"name":"` + tt.srvName + `"},"isp":"` + tt.isp + `"}`)
			result, err := parseOoklaSpeedtestJSON(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Server != tt.want {
				t.Fatalf("server = %q, want %q", result.Server, tt.want)
			}
		})
	}
}

func TestParseOoklaSpeedtestJSONTrimsWhitespace(t *testing.T) {
	tests := []struct {
		name    string
		isp     string
		srvName string
		want    string
	}{
		{"isp leading space", " Test ISP", "Moscow", "Test ISP - Moscow"},
		{"isp trailing space", "Test ISP ", "Moscow", "Test ISP - Moscow"},
		{"name leading space", "Test ISP", " Moscow", "Test ISP - Moscow"},
		{"name trailing space", "Test ISP", "Moscow ", "Test ISP - Moscow"},
		{"both with spaces", "  Test ISP  ", "  Moscow  ", "Test ISP - Moscow"},
		{"isp only with spaces", "  Test ISP  ", "", "Test ISP"},
		{"name only with spaces", "", "  Moscow  ", "Moscow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(`{"ping":{"latency":1},"download":{"bandwidth":1},"upload":{"bandwidth":1},"server":{"name":"` + tt.srvName + `"},"isp":"` + tt.isp + `"}`)
			result, err := parseOoklaSpeedtestJSON(raw)
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

func TestParseOoklaSpeedtestJSONReturnsErrorOnInvalidJSON(t *testing.T) {
	_, err := parseOoklaSpeedtestJSON([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRunSpeedtestUsesFirstSuccessfulBackend(t *testing.T) {
	want := SpeedtestResult{Server: "CLI - Node", PingMS: 1, DownloadMbps: 10, UploadMbps: 5}
	oldCLI := runSpeedtestCLI
	oldOokla := runOoklaSpeedtest
	runSpeedtestCLI = func(ctx context.Context) (SpeedtestResult, error) { return want, nil }
	runOoklaSpeedtest = func(ctx context.Context) (SpeedtestResult, error) { return SpeedtestResult{}, errSpeedtestTest }
	t.Cleanup(func() {
		runSpeedtestCLI = oldCLI
		runOoklaSpeedtest = oldOokla
	})

	req, err := http.NewRequest("GET", "/speedtest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	got, err := RunSpeedtest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestRunSpeedtestFallsBackToOoklaBackend(t *testing.T) {
	want := SpeedtestResult{Server: "Ookla - Node", PingMS: 2, DownloadMbps: 20, UploadMbps: 10}
	oldCLI := runSpeedtestCLI
	oldOokla := runOoklaSpeedtest
	runSpeedtestCLI = func(ctx context.Context) (SpeedtestResult, error) { return SpeedtestResult{}, errSpeedtestTest }
	runOoklaSpeedtest = func(ctx context.Context) (SpeedtestResult, error) { return want, nil }
	t.Cleanup(func() {
		runSpeedtestCLI = oldCLI
		runOoklaSpeedtest = oldOokla
	})

	req, err := http.NewRequest("GET", "/speedtest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	got, err := RunSpeedtest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestRunSpeedtestReturnsUnavailableWhenBothBackendsFail(t *testing.T) {
	oldCLI := runSpeedtestCLI
	oldOokla := runOoklaSpeedtest
	runSpeedtestCLI = func(ctx context.Context) (SpeedtestResult, error) { return SpeedtestResult{}, errSpeedtestTest }
	runOoklaSpeedtest = func(ctx context.Context) (SpeedtestResult, error) { return SpeedtestResult{}, errSpeedtestTest }
	t.Cleanup(func() {
		runSpeedtestCLI = oldCLI
		runOoklaSpeedtest = oldOokla
	})

	req, err := http.NewRequest("GET", "/speedtest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, err = RunSpeedtest(req)
	if !errors.Is(err, ErrSpeedtestUnavailable) {
		t.Fatalf("error = %v, want %v", err, ErrSpeedtestUnavailable)
	}
}

func TestRunSpeedtestCLIReturnsParsedResult(t *testing.T) {
	mockExecCommandContext(t, "speedtest_cli_success")

	got, err := runSpeedtestCLI(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PingMS != 11.2 || got.DownloadMbps != 104 || got.UploadMbps != 52 {
		t.Fatalf("result = %+v", got)
	}
	if got.Server != "Test ISP - Moscow" {
		t.Fatalf("server = %q, want Test ISP - Moscow", got.Server)
	}
}

func TestRunSpeedtestCLIReturnsErrorOnFailure(t *testing.T) {
	mockExecCommandContext(t, "speedtest_cli_failure")

	_, err := runSpeedtestCLI(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "speedtest-cli:") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunOoklaSpeedtestReturnsParsedResult(t *testing.T) {
	mockExecCommandContext(t, "ookla_success")

	got, err := runOoklaSpeedtest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PingMS != 9.5 || got.DownloadMbps != 100 || got.UploadMbps != 50 {
		t.Fatalf("result = %+v", got)
	}
	if got.Server != "Test ISP - Moscow" {
		t.Fatalf("server = %q, want Test ISP - Moscow", got.Server)
	}
}

func TestRunOoklaSpeedtestReturnsErrorOnFailure(t *testing.T) {
	mockExecCommandContext(t, "ookla_failure")

	_, err := runOoklaSpeedtest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "speedtest:") {
		t.Fatalf("error = %v", err)
	}
}
