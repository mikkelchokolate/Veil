package warp

import (
	"github.com/veil-panel/veil/internal/model"
	veilsettings "github.com/veil-panel/veil/internal/settings"
)

type Config = model.WarpConfig

func Redact(warp Config) Config {
	redacted := warp
	disclosure := veilsettings.NewCredentialDisclosure()
	redacted.PrivateKey = disclosure.Redact(redacted.PrivateKey)
	redacted.LicenseKey = disclosure.Redact(redacted.LicenseKey)
	return redacted
}

func PreserveRedacted(update, current Config) Config {
	disclosure := veilsettings.NewCredentialDisclosure()
	update.LicenseKey = disclosure.PreserveRedacted(update.LicenseKey, current.LicenseKey)
	update.PrivateKey = disclosure.PreserveRedacted(update.PrivateKey, current.PrivateKey)
	return update
}

func SetDefaults(warp *Config) {
	if warp.Endpoint == "" {
		warp.Endpoint = "engage.cloudflareclient.com:2408"
	}
	if warp.SocksListen == "" {
		warp.SocksListen = "127.0.0.1"
	}
	if warp.SocksPort == 0 {
		warp.SocksPort = 40000
	}
	if warp.MTU == 0 {
		warp.MTU = 1280
	}
}
