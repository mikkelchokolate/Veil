package installer

import (
	"crypto/rand"
	"io"

	"github.com/veil-panel/veil/internal/panelaccess"
)

type WebBasePathPolicy = panelaccess.WebBasePathPolicy

func NewWebBasePathPolicy(random io.Reader) WebBasePathPolicy {
	return panelaccess.NewWebBasePathPolicy(random)
}

func generateWebBasePath() string {
	return panelaccess.NewWebBasePathPolicy(rand.Reader).Generate()
}
