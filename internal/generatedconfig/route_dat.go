package generatedconfig

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

const maxRouteDatSize = 50 * 1024 * 1024 // 50 MB

var routeDatHTTPClient = &http.Client{Timeout: 30 * time.Second}
var secureRouteDatHTTPClient = newSecureRouteDatHTTPClient()
var routeDatDownloader = DownloadRouteDat

func DownloadRouteDat(url string) ([]byte, error) {
	retry := NewRouteDatRetryPolicy()
	maxAttempts := retry.MaxAttempts()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := retry.Backoff(attempt)
			log.Printf("downloadRouteDat: retry attempt %d/%d after %v (previous error: %v)", attempt, maxAttempts, backoff, lastErr)
			time.Sleep(backoff)
		}
		client := routeDatHTTPClient
		if parsed, parseErr := neturl.Parse(url); parseErr == nil && parsed.Scheme == "https" {
			client = secureRouteDatHTTPClient
		}
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			if !retry.Retryable(err) {
				return nil, err
			}
			continue
		}
		decision := NewRouteDatResponsePolicy().Decide(url, resp.StatusCode, resp.Status)
		if decision.Retry {
			resp.Body.Close()
			lastErr = decision.Err
			continue
		}
		if !decision.Accept {
			resp.Body.Close()
			return nil, decision.Err
		}
		defer resp.Body.Close()
		limit := NewRouteDatBodyLimit(maxRouteDatSize)
		lr := io.LimitReader(resp.Body, int64(limit.Limit())+1)
		body, err := io.ReadAll(lr)
		if err != nil {
			lastErr = err
			if !retry.Retryable(err) {
				return nil, err
			}
			continue
		}
		if err := limit.Validate(url, body); err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, fmt.Errorf("download %s failed after %d attempts: %w", url, maxAttempts, lastErr)
}

func newSecureRouteDatHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, resolved := range addresses {
			if resolved.IsGlobalUnicast() && !resolved.IsPrivate() && !resolved.IsLoopback() && !resolved.IsLinkLocalUnicast() && !resolved.IsUnspecified() {
				return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
			}
		}
		return nil, fmt.Errorf("route data host %q resolves only to non-public addresses", host)
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil || strings.Contains(req.URL.Host, "@") {
				return fmt.Errorf("unsafe route data redirect")
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func downloadRouteDat(url string) ([]byte, error) {
	return DownloadRouteDat(url)
}
