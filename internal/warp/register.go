package warp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
)

// test hooks; overridden in tests only.
var (
	randReader  = rand.Reader
	osHostname  = os.Hostname
	x25519      = curve25519.X25519
	jsonMarshal = json.Marshal
)

const (
	defaultRegBaseURL = "https://api.cloudflareclient.com/v0a4005"
	defaultClientVer  = "a-6.30-3596"
	defaultEndpoint   = DefaultEndpoint
	// registrarEndpointDomain is the only DNS zone a registration response
	// may point the WireGuard peer at. See registrarEndpoint.
	registrarEndpointDomain = "cloudflareclient.com"
	// maxRegistrationResponseBytes caps Cloudflare registration JSON. The
	// payload is a small device/config object; hundreds of KiB is ample.
	maxRegistrationResponseBytes = 256 << 10
)

// DefaultEndpoint is the public Cloudflare WARP WireGuard endpoint used when
// neither the operator nor the registrar supplies one.
const DefaultEndpoint = "engage.cloudflareclient.com:2408"

// Registration is a provisioned Cloudflare WARP account, ready to drop into a
// WarpConfig. It is obtained for free, with no user input, by registering a
// generated WireGuard public key.
type Registration struct {
	PrivateKey    string
	PeerPublicKey string
	Endpoint      string
	LocalAddress  string
	Reserved      []int
	License       string
	DeviceID      string
	AccessToken   string
}

// GenerateKeypair returns base64-encoded WireGuard (curve25519) private and
// public keys.
func GenerateKeypair() (privateKey string, publicKey string, err error) {
	var priv [32]byte
	if _, err := io.ReadFull(randReader, priv[:]); err != nil {
		return "", "", fmt.Errorf("warp: generate private key: %w", err)
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := x25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("warp: derive public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub), nil
}

// Registrar registers free Cloudflare WARP accounts. The zero value is usable;
// BaseURL/Client/ClientVer are overridable for testing.
type Registrar struct {
	Client    *http.Client
	BaseURL   string
	ClientVer string
	Hostname  string
}

func NewRegistrar() *Registrar { return &Registrar{} }

func (r *Registrar) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (r *Registrar) baseURL() string {
	if strings.TrimSpace(r.BaseURL) != "" {
		return strings.TrimRight(r.BaseURL, "/")
	}
	return defaultRegBaseURL
}

func (r *Registrar) clientVer() string {
	if r.ClientVer != "" {
		return r.ClientVer
	}
	return defaultClientVer
}

func (r *Registrar) hostname() string {
	if r.Hostname != "" {
		return r.Hostname
	}
	if h, err := osHostname(); err == nil {
		return h
	}
	return "veil"
}

type warpRegResponse struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Account struct {
		License string `json:"license"`
	} `json:"account"`
	Config struct {
		ClientID  string `json:"client_id"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		Peers []struct {
			PublicKey string `json:"public_key"`
			Endpoint  struct {
				Host string `json:"host"`
			} `json:"endpoint"`
		} `json:"peers"`
	} `json:"config"`
}

// Register generates a keypair and registers a new WARP account, returning the
// fields needed to build a WireGuard/sing-box config.
func (r *Registrar) Register(ctx context.Context) (Registration, error) {
	privateKey, publicKey, err := GenerateKeypair()
	if err != nil {
		return Registration{}, err
	}
	reqBody, err := jsonMarshal(map[string]any{
		"key":   publicKey,
		"tos":   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"type":  "PC",
		"model": "veil",
		"name":  r.hostname(),
	})
	if err != nil {
		return Registration{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL()+"/reg", bytes.NewReader(reqBody))
	if err != nil {
		return Registration{}, err
	}
	req.Header.Set("CF-Client-Version", r.clientVer())
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client().Do(req)
	if err != nil {
		return Registration{}, fmt.Errorf("warp: register request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistrationResponseBytes+1))
	if err != nil {
		return Registration{}, err
	}
	if len(body) > maxRegistrationResponseBytes {
		return Registration{}, fmt.Errorf("warp: registration response exceeds %d-byte limit", maxRegistrationResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return Registration{}, fmt.Errorf("warp: registration failed (status %d): %s", resp.StatusCode, msg)
	}

	var parsed warpRegResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Registration{}, fmt.Errorf("warp: decode registration: %w", err)
	}
	if parsed.ID == "" || len(parsed.Config.Peers) == 0 || parsed.Config.Peers[0].PublicKey == "" {
		return Registration{}, fmt.Errorf("warp: incomplete registration response")
	}

	// Everything below comes from the registration API and is persisted
	// straight into the WARP egress config, so it is validated here rather
	// than left for `sing-box check` to reject opaquely at apply time (#1292).
	peerPublicKey := parsed.Config.Peers[0].PublicKey
	if err := validateWireGuardKey(peerPublicKey); err != nil {
		return Registration{}, fmt.Errorf("warp: registration peer public_key: %w", err)
	}
	endpoint := registrarEndpoint(parsed.Config.Peers[0].Endpoint.Host)
	reserved, err := decodeReserved(parsed.Config.ClientID)
	if err != nil {
		return Registration{}, err
	}
	if len(reserved) != 0 && len(reserved) != 3 {
		return Registration{}, fmt.Errorf("warp: registration client_id must decode to 3 reserved bytes, got %d", len(reserved))
	}
	addresses := []string{}
	if v4 := parsed.Config.Interface.Addresses.V4; v4 != "" {
		addr, err := parseInterfaceAddress(v4, true)
		if err != nil {
			return Registration{}, fmt.Errorf("warp: registration interface v4 address: %w", err)
		}
		addresses = append(addresses, addr.String()+"/32")
	}
	if v6 := parsed.Config.Interface.Addresses.V6; v6 != "" {
		addr, err := parseInterfaceAddress(v6, false)
		if err != nil {
			return Registration{}, fmt.Errorf("warp: registration interface v6 address: %w", err)
		}
		addresses = append(addresses, addr.String()+"/128")
	}
	if len(addresses) == 0 {
		return Registration{}, fmt.Errorf("warp: registration response has no interface addresses")
	}

	return Registration{
		PrivateKey:    privateKey,
		PeerPublicKey: peerPublicKey,
		Endpoint:      endpoint,
		LocalAddress:  strings.Join(addresses, ","),
		Reserved:      reserved,
		License:       parsed.Account.License,
		DeviceID:      parsed.ID,
		AccessToken:   parsed.Token,
	}, nil
}

// decodeReserved turns the base64 client_id (3 bytes) into the reserved bytes
// the WireGuard/WARP peer expects.
func decodeReserved(clientID string) ([]int, error) {
	if clientID == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil {
		return nil, fmt.Errorf("warp: decode client_id: %w", err)
	}
	reserved := make([]int, 0, len(raw))
	for _, b := range raw {
		reserved = append(reserved, int(b))
	}
	return reserved, nil
}

// validateWireGuardKey requires a standard-base64 32-byte curve25519 key.
func validateWireGuardKey(key string) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("not base64: %w", err)
	}
	if len(raw) != curve25519.PointSize {
		return fmt.Errorf("must decode to %d bytes, got %d", curve25519.PointSize, len(raw))
	}
	return nil
}

// parseInterfaceAddress parses a bare interface address of the expected
// family. Prefixes, zones and IPv4-mapped IPv6 forms are rejected, as is
// anything that cannot be a tunnel source (unspecified, multicast, loopback).
func parseInterfaceAddress(raw string, wantV4 bool) (netip.Addr, error) {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, err
	}
	switch {
	case addr.Zone() != "":
		return netip.Addr{}, fmt.Errorf("%q must not carry a zone", raw)
	case wantV4 && !addr.Is4():
		return netip.Addr{}, fmt.Errorf("%q is not an IPv4 address", raw)
	case !wantV4 && (!addr.Is6() || addr.Is4In6()):
		return netip.Addr{}, fmt.Errorf("%q is not an IPv6 address", raw)
	case addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback():
		return netip.Addr{}, fmt.Errorf("%q cannot be a tunnel interface address", raw)
	}
	return addr, nil
}

// registrarEndpoint treats the registration response's peer endpoint as
// advisory. It is accepted only as host:port with a valid port and a host
// inside Cloudflare's cloudflareclient.com zone; anything else (an IP
// literal, a foreign hostname, garbage) falls back to DefaultEndpoint. A
// tampered or compromised registration response therefore cannot repoint
// the persisted WARP egress at an arbitrary host (#1292).
func registrarEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultEndpoint
	}
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		return defaultEndpoint
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return defaultEndpoint
	}
	host = strings.ToLower(host)
	if host != registrarEndpointDomain {
		sub, ok := strings.CutSuffix(host, "."+registrarEndpointDomain)
		if !ok {
			return defaultEndpoint
		}
		for _, label := range strings.Split(sub, ".") {
			if !validDNSLabel(label) {
				return defaultEndpoint
			}
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func validDNSLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
