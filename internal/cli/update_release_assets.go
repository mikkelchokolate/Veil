package cli

import "fmt"

type UpdateArchive struct {
	Name string
	Body []byte
}

type UpdateReleaseAssets struct {
	release    *githubRelease
	downloader func(string) ([]byte, error)
	assetName  string
}

func NewUpdateReleaseAssets(release *githubRelease, downloader func(string) ([]byte, error)) UpdateReleaseAssets {
	return UpdateReleaseAssets{
		release:    release,
		downloader: downloader,
		assetName:  updateAssetName(),
	}
}

func (a UpdateReleaseAssets) DownloadVerifiedArchive() (UpdateArchive, error) {
	checksumsName := "checksums.txt"
	assetURL := findAssetURL(a.release.Assets, a.assetName)
	checksumsURL := findAssetURL(a.release.Assets, checksumsName)
	if assetURL == "" {
		return UpdateArchive{}, fmt.Errorf("release %s has no asset %s", a.release.TagName, a.assetName)
	}
	if checksumsURL == "" {
		return UpdateArchive{}, fmt.Errorf("release %s has no checksums asset", a.release.TagName)
	}
	archive, err := a.downloader(assetURL)
	if err != nil {
		return UpdateArchive{}, fmt.Errorf("download %s: %w", a.assetName, err)
	}
	checksumsBody, err := a.downloader(checksumsURL)
	if err != nil {
		return UpdateArchive{}, fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyAssetChecksum(archive, a.assetName, string(checksumsBody)); err != nil {
		return UpdateArchive{}, fmt.Errorf("checksum verification failed: %w", err)
	}
	return UpdateArchive{Name: a.assetName, Body: archive}, nil
}

func downloadVerifiedUpdateAsset(release *githubRelease) (string, []byte, error) {
	archive, err := NewUpdateReleaseAssets(release, updateAssetDownloader).DownloadVerifiedArchive()
	if err != nil {
		return "", nil, err
	}
	return archive.Name, archive.Body, nil
}
