package cli

import (
	"net/http"
	"time"

	versionflow "github.com/veil-panel/veil/internal/cliflow/version"
)

var versionCheckClient = &http.Client{Timeout: 10 * time.Second}

func fetchLatestReleaseTag() (string, error) {
	old := versionflow.HTTPClient
	versionflow.HTTPClient = versionCheckClient
	defer func() { versionflow.HTTPClient = old }()
	return versionflow.FetchLatestReleaseTag()
}

func compareVersions(a, b string) int {
	return versionflow.Compare(a, b)
}
