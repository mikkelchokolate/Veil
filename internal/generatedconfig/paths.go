package generatedconfig

import "path/filepath"

type Paths struct {
	ApplyRoot string
}

type GeneratedConfigPaths = Paths

func NewPaths(applyRoot string) Paths {
	return Paths{ApplyRoot: applyRoot}
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
