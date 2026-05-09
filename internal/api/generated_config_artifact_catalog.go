package api

import "github.com/veil-panel/veil/internal/generatedconfig"

const (
	generatedCaddyfileSubpath       = generatedconfig.CaddyfileSubpath
	generatedHysteria2ConfigSubpath = generatedconfig.Hysteria2ConfigSubpath
	generatedMieruConfigSubpath     = generatedconfig.MieruConfigSubpath
	generatedWarpConfigSubpath      = generatedconfig.WarpConfigSubpath
)

type GeneratedConfigArtifactSpec = generatedconfig.ArtifactSpec
type GeneratedConfigArtifactCatalog = generatedconfig.ArtifactCatalog

func NewGeneratedConfigArtifactCatalog() GeneratedConfigArtifactCatalog {
	return generatedconfig.NewArtifactCatalog()
}
