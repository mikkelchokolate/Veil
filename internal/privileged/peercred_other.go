//go:build !linux

package privileged

import "context"

// PeerPolicy describes which local processes may use the root helper socket.
// Only Linux builds can enforce it; other platforms fail closed.
type PeerPolicy struct {
	AllowedUID  uint32
	AllowRoot   bool
	AllowedUnit string
}

func (s *Server) ServeUnix(context.Context, string, PeerPolicy) error {
	return ErrUnixPeerCredentialsUnsupported
}

func (s *Server) ServeSystemd(context.Context, PeerPolicy) error {
	return ErrUnixPeerCredentialsUnsupported
}
