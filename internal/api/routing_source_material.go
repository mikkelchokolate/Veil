package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type RoutingSourceDownloader = generatedconfig.RoutingSourceDownloader
type RoutingSourceMaterial = generatedconfig.RoutingSourceMaterial

var routeDatDownloader = generatedconfig.DownloadRouteDat

func NewRoutingSourceMaterial(applyRoot string, source RoutingSource) RoutingSourceMaterial {
	return generatedconfig.NewRoutingSourceMaterial(applyRoot, source).WithDownloader(routeDatDownloader)
}

func downloadRouteDat(url string) ([]byte, error) { return generatedconfig.DownloadRouteDat(url) }

func verifyRouteDatChecksum(filename string, body []byte, checksumText string) error {
	return generatedconfig.VerifyRouteDatChecksum(filename, body, checksumText)
}

func fetchVerifiedRouteDatFile(file RoutingSourceFile) ([]byte, error) {
	return NewRoutingSourceMaterial("", RoutingSource{}).Fetch(file)
}
