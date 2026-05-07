package cli

import "fmt"

func downloadVerifiedUpdateAsset(release *githubRelease) (string, []byte, error) {
	assetName := updateAssetName()
	checksumsName := "checksums.txt"
	assetURL := findAssetURL(release.Assets, assetName)
	checksumsURL := findAssetURL(release.Assets, checksumsName)
	if assetURL == "" {
		return "", nil, fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
	}
	if checksumsURL == "" {
		return "", nil, fmt.Errorf("release %s has no checksums asset", release.TagName)
	}
	archive, err := updateAssetDownloader(assetURL)
	if err != nil {
		return "", nil, fmt.Errorf("download %s: %w", assetName, err)
	}
	checksumsBody, err := updateAssetDownloader(checksumsURL)
	if err != nil {
		return "", nil, fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyAssetChecksum(archive, assetName, string(checksumsBody)); err != nil {
		return "", nil, fmt.Errorf("checksum verification failed: %w", err)
	}
	return assetName, archive, nil
}
