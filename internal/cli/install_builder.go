package cli

import "github.com/veil-panel/veil/internal/installer"

func buildRURecommendedInstallFromOptions(opts ruRecommendedInstallOptions) (installer.RURecommendedInstall, error) {
	return installer.BuildRURecommendedInstall(installer.RURecommendedInstallInput{
		Domain:          opts.Domain,
		Email:           opts.Email,
		PanelAccess:     opts.PanelAccess,
		PanelPort:       opts.PanelPort,
		Secret:          randomSecret,
		RandomPanelPort: installer.RandomHighPort,
	})
}
