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

// runetfreedom geosite.dat is currently ~74MiB; keep headroom for growth.
const maxRouteDatSize = 128 * 1024 * 1024

var routeDatSizeCap = maxRouteDatSize

var routeDatHTTPClient = &http.Client{Timeout: 120 * time.Second}
var secureRouteDatHTTPClient = newSecureRouteDatHTTPClient()

func DownloadRouteDat(url string) ([]byte, error) {
	return DownloadRouteDatContext(context.Background(), url)
}

func DownloadRouteDatContext(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || !routingSourceHostAllowed(parsed.Hostname()) {
		return nil, fmt.Errorf("route data URL must use an allowed HTTPS source")
	}
	return downloadRouteDatContextUnchecked(ctx, rawURL)
}

func downloadRouteDatContextUnchecked(ctx context.Context, url string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	retry := NewRouteDatRetryPolicy()
	maxAttempts := retry.MaxAttempts()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := retry.Backoff(attempt)
			log.Printf("downloadRouteDat: retry attempt %d/%d after %v (previous error: %v)", attempt, maxAttempts, backoff, lastErr)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		client := routeDatHTTPClient
		if parsed, parseErr := neturl.Parse(url); parseErr == nil && parsed.Scheme == "https" {
			client = secureRouteDatHTTPClient
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(request)
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
		limit := NewRouteDatBodyLimit(routeDatSizeCap)
		lr := io.LimitReader(resp.Body, int64(limit.Limit())+1)
		body, readErr := io.ReadAll(lr)
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if !retry.Retryable(readErr) {
				return nil, readErr
			}
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
			if !retry.Retryable(closeErr) {
				return nil, closeErr
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

func downloadRouteDatContext(ctx context.Context, url string) ([]byte, error) {
	return downloadRouteDatContextUnchecked(ctx, url)
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
		Timeout:   120 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil || strings.Contains(req.URL.Host, "@") || !routingSourceHostAllowed(req.URL.Hostname()) {
				return fmt.Errorf("unsafe or disallowed route data redirect")
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func downloadRouteDat(url string) ([]byte, error) {
	return downloadRouteDatContextUnchecked(context.Background(), url)
}
