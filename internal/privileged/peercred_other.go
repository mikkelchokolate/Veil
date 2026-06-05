//go:build !linux

package privileged

import "context"

func (s *Server) ServeUnix(context.Context, string, uint32, bool) error {
	return ErrUnixPeerCredentialsUnsupported
}
