package warp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	defaultEndpoint   = "engage.cloudflareclient.com:2408"
)

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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Registration{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Registration{}, fmt.Errorf("warp: registration failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed warpRegResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Registration{}, fmt.Errorf("warp: decode registration: %w", err)
	}
	if parsed.ID == "" || len(parsed.Config.Peers) == 0 || parsed.Config.Peers[0].PublicKey == "" {
		return Registration{}, fmt.Errorf("warp: incomplete registration response")
	}

	endpoint := parsed.Config.Peers[0].Endpoint.Host
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	reserved, err := decodeReserved(parsed.Config.ClientID)
	if err != nil {
		return Registration{}, err
	}
	addresses := []string{}
	if v4 := parsed.Config.Interface.Addresses.V4; v4 != "" {
		addresses = append(addresses, v4+"/32")
	}
	if v6 := parsed.Config.Interface.Addresses.V6; v6 != "" {
		addresses = append(addresses, v6+"/128")
	}
	if len(addresses) == 0 {
		return Registration{}, fmt.Errorf("warp: registration response has no interface addresses")
	}

	return Registration{
		PrivateKey:    privateKey,
		PeerPublicKey: parsed.Config.Peers[0].PublicKey,
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
