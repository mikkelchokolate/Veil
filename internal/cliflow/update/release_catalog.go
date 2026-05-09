package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Release represents a subset of the GitHub Release API response.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ReleaseCatalog struct {
	Owner      string
	Repo       string
	BaseURL    string
	HTTPClient *http.Client
}

func NewReleaseCatalog(owner, repo string) ReleaseCatalog {
	return ReleaseCatalog{
		Owner:      owner,
		Repo:       repo,
		BaseURL:    "https://api.github.com",
		HTTPClient: HTTPClient,
	}
}

func (c ReleaseCatalog) Latest() (*Release, error) {
	client := c.HTTPClient
	if client == nil {
		client = HTTPClient
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", baseURL, c.Owner, c.Repo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release JSON: %w", err)
	}
	return &release, nil
}

// FetchLatestRelease queries the GitHub API for the latest release.
func FetchLatestRelease() (*Release, error) {
	return NewReleaseCatalog(RepoOwner, RepoName).Latest()
}
