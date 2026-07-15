package generatedconfig

import "path/filepath"

type Paths struct {
	ApplyRoot string
	// LiveRoot is the production filesystem root where promoted/live generated
	// config artifacts reside. When empty, ApplyRoot is used for backwards
	// compatibility/tests.
	LiveRoot string
	// EtcRoot is the persistent configuration root (e.g. /etc/veil) where
	// non-generated files such as synced TLS certificates and panel TLS keys
	// live. When empty, ApplyRoot is used for backwards compatibility/tests.
	EtcRoot string
}

type GeneratedConfigPaths = Paths

func NewPaths(applyRoot string) Paths {
	return Paths{ApplyRoot: applyRoot, LiveRoot: applyRoot, EtcRoot: applyRoot}
}

func NewPathsWithLiveRoot(applyRoot, liveRoot string) Paths {
	etcRoot := applyRoot
	if liveRoot != "" {
		etcRoot = filepath.Dir(liveRoot)
	}
	return Paths{ApplyRoot: applyRoot, LiveRoot: liveRoot, EtcRoot: etcRoot}
}

func NewGeneratedConfigPaths(applyRoot string) GeneratedConfigPaths { return NewPaths(applyRoot) }

func (p Paths) Generated(subpath string) string {
	return filepath.Join(p.ApplyRoot, "generated", filepath.FromSlash(subpath))
}

func (p Paths) CaddyJSON() string {
	return p.Generated(CaddyJSONConfigSubpath)
}

func (p Paths) Hysteria2() string {
	return p.Generated(Hysteria2ConfigSubpath)
}

func (p Paths) Mieru() string {
	return p.Generated(MieruConfigSubpath)
}

func (p Paths) Warp() string {
	return p.Generated(WarpConfigSubpath)
}

func (p Paths) etcRoot() string {
	if p.EtcRoot != "" {
		return p.EtcRoot
	}
	return p.ApplyRoot
}

func (p Paths) CertPath(domain string) string {
	return filepath.Join(p.etcRoot(), "certs", domain+".crt")
}

func (p Paths) KeyPath(domain string) string {
	return filepath.Join(p.etcRoot(), "certs", domain+".key")
}

func (p Paths) PanelCertPath() string {
	return filepath.Join(p.etcRoot(), "panel", "tls.crt")
}

func (p Paths) PanelKeyPath() string {
	return filepath.Join(p.etcRoot(), "panel", "tls.key")
}
