package api

type WarpManagement struct {
	warp *WarpConfig
	save func() error
}

func NewWarpManagement(warp *WarpConfig, save func() error) WarpManagement {
	if save == nil {
		save = func() error { return nil }
	}
	return WarpManagement{warp: warp, save: save}
}

func (m WarpManagement) Get() WarpConfig {
	if m.warp == nil {
		return WarpConfig{}
	}
	return redactedWarp(*m.warp)
}

func (m WarpManagement) Update(update WarpConfig) (WarpConfig, error) {
	current := WarpConfig{}
	if m.warp != nil {
		current = *m.warp
	}
	if update.LicenseKey == "[REDACTED]" {
		update.LicenseKey = current.LicenseKey
	}
	if update.PrivateKey == "[REDACTED]" {
		update.PrivateKey = current.PrivateKey
	}
	setWarpDefaults(&update)
	if m.warp != nil {
		*m.warp = update
	}
	if err := m.save(); err != nil {
		return WarpConfig{}, err
	}
	return redactedWarp(update), nil
}

func redactedWarp(warp WarpConfig) WarpConfig {
	redacted := warp
	if redacted.PrivateKey != "" {
		redacted.PrivateKey = "[REDACTED]"
	}
	if redacted.LicenseKey != "" {
		redacted.LicenseKey = "[REDACTED]"
	}
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
