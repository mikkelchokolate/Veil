package status

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

const containerHealthPathEnv = "VEIL_CONTAINER_HEALTH_PATH"

// ContainerHealthContract is the serve/probe agreement written by `veil serve`
// when VEIL_CONTAINER_HEALTH_PATH is set. It never stores credentials.
type ContainerHealthContract struct {
	Listen      string `json:"listen"`
	Scheme      string `json:"scheme"`
	WebBasePath string `json:"webBasePath"`
	TLSCert     string `json:"tlsCert,omitempty"`
	ServerName  string `json:"serverName,omitempty"`
}

// ContractPathFromEnv returns the explicitly configured health contract path —
// empty when VEIL_CONTAINER_HEALTH_PATH is unset. `veil serve` writes the
// contract only when this is set, keeping the file opt-in for container
// deployments (the image entrypoint exports it).
func ContractPathFromEnv() string {
	return strings.TrimSpace(os.Getenv(containerHealthPathEnv))
}

// ContractProbePath resolves the path `veil healthcheck` reads. An explicit
// VEIL_CONTAINER_HEALTH_PATH wins; otherwise it derives from the state root —
// VEIL_VAR_DIR, then the VEIL_STATE_PATH parent, then the packaged default —
// mirroring the entrypoint leaf-path derivation. Docker HEALTHCHECK execs do
// not pass through the entrypoint, so the probe must derive the same path the
// serve side wrote rather than relying on an exported variable (issue #753).
func ContractProbePath() string {
	if explicit := ContractPathFromEnv(); explicit != "" {
		return explicit
	}
	return filepath.Join(hostenv.VarDir(), "container-health.json")
}

func ContractFromServe(listen string, tlsEnabled bool, webBasePath, tlsCert, serverName string) ContainerHealthContract {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	return ContainerHealthContract{
		Listen:      strings.TrimSpace(listen),
		Scheme:      scheme,
		WebBasePath: webBasePath,
		TLSCert:     strings.TrimSpace(tlsCert),
		ServerName:  strings.TrimSpace(serverName),
	}
}

func WriteContract(path string, contract ContainerHealthContract) error {
	body, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(body, '\n'), 0o600, 0o755)
}

func ReadContract(path string) (ContainerHealthContract, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ContainerHealthContract{}, fmt.Errorf("read container health contract: %w", err)
	}
	var contract ContainerHealthContract
	if err := json.Unmarshal(body, &contract); err != nil {
		return ContainerHealthContract{}, fmt.Errorf("parse container health contract: %w", err)
	}
	if strings.TrimSpace(contract.Listen) == "" {
		return ContainerHealthContract{}, fmt.Errorf("container health contract is missing listen")
	}
	if contract.Scheme != "http" && contract.Scheme != "https" {
		return ContainerHealthContract{}, fmt.Errorf("container health contract has invalid scheme %q", contract.Scheme)
	}
	return contract, nil
}

func LocalHealthURL(contract ContainerHealthContract) (string, error) {
	addr, err := localProbeAddr(contract.Listen)
	if err != nil {
		return "", err
	}
	basePath, err := webbasepath.Normalize(contract.WebBasePath)
	if err != nil {
		return "", fmt.Errorf("container health web base path: %w", err)
	}
	origin := contract.Scheme + "://" + addr
	return strings.TrimRight(origin, "/") + basePath + "healthz", nil
}

func localProbeAddr(listen string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return "", fmt.Errorf("container health listen: %w", err)
	}
	return net.JoinHostPort(rewriteUnspecifiedHost(host), port), nil
}

func Probe(ctx context.Context, contract ContainerHealthContract, token string) error {
	url, err := LocalHealthURL(contract)
	if err != nil {
		return err
	}
	client, err := HealthHTTPClient(contract)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("X-Veil-Token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("container health probe %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("container health probe %s: %s", url, resp.Status)
	}
	// A bare 200 is not enough: the endpoint must be the Veil health probe
	// reporting status "ok", otherwise an unrelated service or an unhealthy
	// payload could be mistaken for readiness.
	if !HealthzStatusOK(body) {
		return fmt.Errorf("container health probe %s: expected status \"ok\" body, got %q", url, strings.TrimSpace(string(body)))
	}
	return nil
}

// HealthzStatusOK reports whether a /healthz response body is the Veil health
// contract: JSON with status "ok".
func HealthzStatusOK(body []byte) bool {
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Status == "ok"
}

var HealthHTTPClient = defaultHealthHTTPClient

func defaultHealthHTTPClient(contract ContainerHealthContract) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if contract.Scheme == "https" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if contract.ServerName != "" {
			tlsConfig.ServerName = contract.ServerName
		}
		certPath := contract.TLSCert
		if certPath == "" {
			certPath = effectivePanelTLSCertFile()
		}
		if certPath != "" {
			pem, err := os.ReadFile(certPath)
			if err == nil {
				pool, poolErr := x509.SystemCertPool()
				if poolErr != nil || pool == nil {
					pool = x509.NewCertPool()
				}
				if pool.AppendCertsFromPEM(pem) {
					tlsConfig.RootCAs = pool
				}
			} else if contract.TLSCert != "" {
				return nil, fmt.Errorf("read container health TLS cert: %w", err)
			}
		}
		transport.TLSClientConfig = tlsConfig
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: transport}, nil
}
