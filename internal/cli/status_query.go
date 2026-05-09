package cli

import (
	"context"
	"io"

	statusflow "github.com/veil-panel/veil/internal/cliflow/status"
)

type statusQueryOptions = statusflow.Options

type StatusQuery struct {
	inner statusflow.Query
}

func NewStatusQuery(opts statusQueryOptions, out io.Writer) StatusQuery {
	return StatusQuery{inner: statusflow.NewQuery(opts, out, resolveServeAuthToken)}
}

func (q StatusQuery) Run(ctx context.Context) error {
	return q.inner.Run(ctx)
}

func statusCandidateAddrs(addr string) []string {
	return statusflow.CandidateAddrs(addr)
}

func (q StatusQuery) Render(status *statusResponse) error {
	return q.inner.Render(status)
}
