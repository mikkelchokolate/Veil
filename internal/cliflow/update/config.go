package update

import (
	"net/http"
	"time"
)

const (
	RepoOwner = "mikkelchokolate"
	RepoName  = "Veil"
	Timeout   = 5 * time.Minute
)

var HTTPClient = &http.Client{Timeout: 30 * time.Second}
