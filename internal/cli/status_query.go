package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type statusQueryOptions struct {
	Listen    string
	AuthToken string
	JSON      bool
}

type StatusQuery struct {
	opts statusQueryOptions
	out  io.Writer
}

func NewStatusQuery(opts statusQueryOptions, out io.Writer) StatusQuery {
	return StatusQuery{opts: opts, out: out}
}

func (q StatusQuery) Run(ctx context.Context) error {
	addr := resolveStatusListen(q.opts.Listen)
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	token, _ := resolveServeAuthToken(q.opts.AuthToken)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := fetchStatus(ctx, addr+"/api/status", token)
	if err != nil {
		return fmt.Errorf("fetch status from %s: %w", addr, err)
	}
	return q.Render(status)
}

func (q StatusQuery) Render(status *statusResponse) error {
	if q.opts.JSON {
		enc := json.NewEncoder(q.out)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	fmt.Fprintf(q.out, "Veil %s\n", status.Version)
	fmt.Fprintf(q.out, "Mode: %s\n", status.Mode)
	fmt.Fprintln(q.out, "Services:")
	for _, svc := range status.Services {
		state := svc.ActiveState
		if svc.Error != "" {
			state = fmt.Sprintf("%s (error: %s)", state, svc.Error)
		}
		marker := "○"
		if svc.ActiveState == "active" {
			marker = "●"
		} else if svc.ActiveState == "failed" {
			marker = "✕"
		}
		proto := ""
		if svc.Transport != "" {
			proto = fmt.Sprintf(" (%s)", svc.Transport)
		}
		fmt.Fprintf(q.out, "  %s %s%s: %s\n", marker, svc.Name, proto, state)
	}
	return nil
}
