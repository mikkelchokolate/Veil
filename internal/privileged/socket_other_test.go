//go:build !linux

package privileged

import (
	"context"
	"errors"
	"testing"
)

func TestServeUnixReportsUnsupportedPeerCredentials(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	err := server.ServeUnix(context.Background(), "helper.sock", PeerPolicy{AllowedUID: 1000})
	if !errors.Is(err, ErrUnixPeerCredentialsUnsupported) {
		t.Fatalf("want ErrUnixPeerCredentialsUnsupported, got %v", err)
	}
}
