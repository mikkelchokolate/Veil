package livevalidation

import (
	"context"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

type Request struct {
	Settings        model.Settings
	Inbounds        []model.Inbound
	CurrentInbounds []model.Inbound
	Warp            model.WarpConfig
	// RuntimeIdentities carries the normalized Client+Binding identities per
	// inbound (from the SQLite credential store). The generated mieru config
	// aggregates users globally, so identities across inbounds must be unique
	// and within the upstream 64-byte cap even when they never appear in the
	// legacy inbound-embedded profiles (audit #3/#104).
	RuntimeIdentities map[string][]string
}

type Response struct {
	Valid     bool                    `json:"valid"`
	Issues    []model.ValidationIssue `json:"issues"`
	CheckedAt time.Time               `json:"checkedAt"`
}

type PortProbe interface {
	Available(context.Context, string, int) (bool, error)
}

type DNSResolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

type BinaryLookup interface {
	LookPath(string) (string, error)
}

type UnitInspector interface {
	Exists(context.Context, string) (bool, error)
}

type Validator struct {
	Ports    PortProbe
	DNS      DNSResolver
	Binaries BinaryLookup
	Units    UnitInspector
	Now      func() time.Time
}
