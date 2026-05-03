package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var errSpeedtestUnavailable = errors.New("speedtest unavailable")

type SpeedtestResult struct {
	Server       string  `json:"server,omitempty"`
	PingMS       float64 `json:"pingMs"`
	DownloadMbps float64 `json:"downloadMbps"`
	UploadMbps   float64 `json:"uploadMbps"`
	Raw          string  `json:"raw,omitempty"`
}

var speedtestRunner = runSpeedtest

func runSpeedtest(r *http.Request) (SpeedtestResult, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	if result, err := runSpeedtestCLI(ctx); err == nil {
		return result, nil
	}
	if result, err := runOoklaSpeedtest(ctx); err == nil {
		return result, nil
	}
	return SpeedtestResult{}, errSpeedtestUnavailable
}

func runSpeedtestCLI(ctx context.Context) (SpeedtestResult, error) {
	out, err := exec.CommandContext(ctx, "speedtest-cli", "--json").CombinedOutput()
	if err != nil {
		return SpeedtestResult{}, fmt.Errorf("speedtest-cli: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseSpeedtestCLIJSON(out)
}

func runOoklaSpeedtest(ctx context.Context) (SpeedtestResult, error) {
	out, err := exec.CommandContext(ctx, "speedtest", "--accept-license", "--accept-gdpr", "--format=json").CombinedOutput()
	if err != nil {
		return SpeedtestResult{}, fmt.Errorf("speedtest: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseOoklaSpeedtestJSON(out)
}

func parseSpeedtestCLIJSON(raw []byte) (SpeedtestResult, error) {
	var payload struct {
		Ping     float64 `json:"ping"`
		Download float64 `json:"download"`
		Upload   float64 `json:"upload"`
		Server   struct {
			Sponsor string `json:"sponsor"`
			Name    string `json:"name"`
		} `json:"server"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return SpeedtestResult{}, err
	}
	serverLabel := payload.Server.Name
	if payload.Server.Sponsor != "" && payload.Server.Name != "" {
		serverLabel = payload.Server.Sponsor + " - " + payload.Server.Name
	} else if payload.Server.Sponsor != "" {
		serverLabel = payload.Server.Sponsor
	}
	return SpeedtestResult{
		Server:       serverLabel,
		PingMS:       payload.Ping,
		DownloadMbps: payload.Download / 1_000_000,
		UploadMbps:   payload.Upload / 1_000_000,
		Raw:          string(raw),
	}, nil
}

func parseOoklaSpeedtestJSON(raw []byte) (SpeedtestResult, error) {
	var payload struct {
		Ping struct {
			Latency float64 `json:"latency"`
		} `json:"ping"`
		Download struct {
			Bandwidth float64 `json:"bandwidth"`
		} `json:"download"`
		Upload struct {
			Bandwidth float64 `json:"bandwidth"`
		} `json:"upload"`
		Server struct {
			Name string `json:"name"`
		} `json:"server"`
		ISP string `json:"isp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return SpeedtestResult{}, err
	}
	server := payload.Server.Name
	if payload.ISP != "" && server != "" {
		server = payload.ISP + " - " + server
	} else if payload.ISP != "" {
		server = payload.ISP
	}
	return SpeedtestResult{
		Server:       server,
		PingMS:       payload.Ping.Latency,
		DownloadMbps: payload.Download.Bandwidth * 8 / 1_000_000,
		UploadMbps:   payload.Upload.Bandwidth * 8 / 1_000_000,
		Raw:          string(raw),
	}, nil
}
