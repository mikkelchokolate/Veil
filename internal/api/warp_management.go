package api

type WarpManagement struct {
	mutation ManagementStateMutation
}

func NewWarpManagement(warp *WarpConfig, save func() error) WarpManagement {
	return WarpManagement{mutation: NewManagementStateMutation(ManagementStateMutationTarget{Warp: warp}, save)}
}

func (m WarpManagement) Get() WarpConfig {
	return m.mutation.Warp()
}

func (m WarpManagement) Update(update WarpConfig) (WarpConfig, error) {
	return m.mutation.UpdateWarp(update)
}

func redactedWarp(warp WarpConfig) WarpConfig {
	redacted := warp
	disclosure := NewCredentialDisclosure()
	redacted.PrivateKey = disclosure.Redact(redacted.PrivateKey)
	redacted.LicenseKey = disclosure.Redact(redacted.LicenseKey)
	return redacted
}

func setWarpDefaults(warp *WarpConfig) {
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
