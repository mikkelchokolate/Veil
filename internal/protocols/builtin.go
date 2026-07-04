package protocols

import (
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
	"github.com/mikkelchokolate/Veil/internal/protocols/mieru"
	"github.com/mikkelchokolate/Veil/internal/protocols/naiveproxy"
	"github.com/mikkelchokolate/Veil/internal/protocols/olcrtc"
)

// NewRegistry returns a registry with all built-in protocol plugins registered.
func NewRegistry() *Registry {
	r := NewEmptyRegistry()
	r.Register(naiveproxy.New())
	r.Register(hysteria2.New())
	r.Register(olcrtc.New())
	r.Register(mieru.New())
	return r
}

// NewEmptyRegistry returns an empty registry for tests and custom compositions.
func NewEmptyRegistry() *Registry {
	return NewRegistryRaw()
}
