package caddyadmin

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

const defaultAdminTimeout = 30 * time.Second

type Client struct {
	AdminEndpoint string
	HTTPClient    *http.Client
}

func NewClient(endpoint string) Client {
	if endpoint == "" {
		endpoint = "http://127.0.0.1:2019"
	}
	return Client{AdminEndpoint: endpoint, HTTPClient: &http.Client{Timeout: defaultAdminTimeout}}
}

func (c Client) LoadConfig(json []byte) error {
	url := c.AdminEndpoint + "/load"
	resp, err := c.HTTPClient.Post(url, "application/json", bytes.NewReader(json))
	if err != nil {
		return fmt.Errorf("caddy admin POST failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy admin returned %s", resp.Status)
	}
	return nil
}
