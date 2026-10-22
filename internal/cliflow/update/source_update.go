package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	versionflow "github.com/mikkelchokolate/Veil/internal/cliflow/version"
)

var sourceUpdateTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func fullCommit(sha string) bool {
	decoded, err := hex.DecodeString(sha)
	return err == nil && len(decoded) == 20
}

// ResolveTagCommit peels a bounded annotated-tag chain to an exact commit.
func (c ReleaseCatalog) ResolveTagCommit(ctx context.Context, tag string) (string, error) {
	if c.Owner == "" || c.Repo == "" || tag == "" {
		return "", errors.New("GitHub tag resolver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	path := "git/ref/tags/" + url.PathEscape(tag)
	for depth := 0; depth < 3; depth++ {
		var result struct {
			Object struct{ SHA, Type string } `json:"object"`
		}
		if err := c.sourceUpdateJSON(ctx, path, 64<<10, &result); err != nil {
			return "", err
		}
		sha := strings.ToLower(strings.TrimSpace(result.Object.SHA))
		if !fullCommit(sha) {
			return "", errors.New("GitHub tag resolved to an invalid commit identity")
		}
		switch result.Object.Type {
		case "commit":
			return sha, nil
		case "tag":
			path = "git/tags/" + sha
		default:
			return "", fmt.Errorf("GitHub tag resolved to unsupported object type %q", result.Object.Type)
		}
	}
	return "", errors.New("GitHub annotated tag chain exceeds maximum depth")
}

// SourceUpdateCommit permits only a forward source-to-release transition.
// Callers must bind the returned SHA to the signed release provenance too.
func (c ReleaseCatalog) SourceUpdateCommit(ctx context.Context, current, tag string) (string, error) {
	base := versionflow.SourceBuildCommit(current)
	if base == "" {
		return "", errors.New("running build has no unambiguous main source commit")
	}
	if !sourceUpdateTagPattern.MatchString(tag) || !versionflow.IsReleaseVersion(tag) {
		return "", errors.New("update target is not a canonical release tag")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	target, err := c.ResolveTagCommit(ctx, tag)
	if err != nil {
		return "", fmt.Errorf("resolve release source commit: %w", err)
	}
	var comparison struct {
		Status          string               `json:"status"`
		BaseCommit      struct{ SHA string } `json:"base_commit"`
		MergeBaseCommit struct{ SHA string } `json:"merge_base_commit"`
	}
	// Compare immutable SHAs, not a tag that may move between lookup and proof.
	if err := c.sourceUpdateJSON(ctx, "compare/"+base+"..."+target, 8<<20, &comparison); err != nil {
		return "", fmt.Errorf("compare release source ancestry: %w", err)
	}
	if (comparison.Status != "ahead" && comparison.Status != "identical") ||
		comparison.BaseCommit.SHA != base || comparison.MergeBaseCommit.SHA != base ||
		(comparison.Status == "identical") != (target == base) {
		return "", errors.New("release source does not contain the running main build; update may be a downgrade")
	}
	return target, nil
}

func (c ReleaseCatalog) sourceUpdateJSON(ctx context.Context, path string, limit int64, result any) error {
	client := c.HTTPClient
	if client == nil {
		client = HTTPClient
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/%s", baseURL, url.PathEscape(c.Owner), url.PathEscape(c.Repo), path)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "veil")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub source lookup status %d", response.StatusCode)
	}
	if int64(len(body)) > limit {
		return errors.New("GitHub source lookup response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("parse GitHub source lookup: %w", err)
	}
	return nil
}
