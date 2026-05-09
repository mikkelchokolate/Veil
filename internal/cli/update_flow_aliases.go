package cli

import updateflow "github.com/veil-panel/veil/internal/cliflow/update"

type githubRelease = updateflow.Release
type githubAsset = updateflow.Asset
type UpdateArchive = updateflow.Archive
type UpdateReleaseAssets = updateflow.ReleaseAssets
type UpdateReleaseCatalog = updateflow.ReleaseCatalog
type UpdateReleaseArchive = updateflow.ReleaseArchive
type UpdateBinaryFiles = updateflow.BinaryFiles

func NewUpdateReleaseAssets(release *githubRelease, downloader func(string) ([]byte, error)) UpdateReleaseAssets {
	return updateflow.NewReleaseAssets(release, downloader)
}

func NewUpdateReleaseCatalog(owner, repo string) UpdateReleaseCatalog {
	catalog := updateflow.NewReleaseCatalog(owner, repo)
	catalog.HTTPClient = updateHTTPClient
	return catalog
}

func NewUpdateReleaseArchive(body []byte) UpdateReleaseArchive {
	return updateflow.NewReleaseArchive(body)
}

func NewUpdateBinaryFiles() UpdateBinaryFiles {
	return updateflow.NewBinaryFiles()
}

func updateAssetName() string { return updateflow.AssetName() }

func findAssetURL(assets []githubAsset, name string) string {
	return updateflow.FindAssetURL(assets, name)
}

func downloadAsset(url string) ([]byte, error) {
	updateflow.HTTPClient = updateHTTPClient
	return updateflow.DownloadAsset(url)
}

func verifyAssetChecksum(archive []byte, assetName, checksumsText string) error {
	return updateflow.VerifyAssetChecksum(archive, assetName, checksumsText)
}

func extractChecksumForFile(checksumsText, filename string) string {
	return updateflow.ExtractChecksumForFile(checksumsText, filename)
}

func fetchLatestRelease() (*githubRelease, error) {
	return NewUpdateReleaseCatalog(updateRepoOwner, updateRepoName).Latest()
}

func downloadVerifiedUpdateAsset(release *githubRelease) (string, []byte, error) {
	archive, err := NewUpdateReleaseAssets(release, updateAssetDownloader).DownloadVerifiedArchive()
	if err != nil {
		return "", nil, err
	}
	return archive.Name, archive.Body, nil
}

func extractVeilBinary(archive []byte) ([]byte, error) {
	return updateflow.ExtractVeilBinary(archive)
}

func copyFileData(src, dst string) error {
	return updateflow.CopyFileData(src, dst)
}

func replaceBinaryAtomic(dst string, data []byte) error {
	return updateflow.ReplaceBinaryAtomic(dst, data)
}

func rollbackBinary(backupPath, currentPath string) error {
	return updateflow.RollbackBinary(backupPath, currentPath)
}
