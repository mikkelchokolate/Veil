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
