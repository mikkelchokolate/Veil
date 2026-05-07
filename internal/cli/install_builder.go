package cli

import "github.com/veil-panel/veil/internal/installer"

func buildRURecommendedInstallFromOptions(opts ruRecommendedInstallOptions) (installer.RURecommendedInstall, error) {
	availability, err := installer.DetectPortAvailability([]int{443, 8443})
	if err != nil {
		return installer.RURecommendedInstall{}, err
	}
	randomPort := func() int {
		port, err := installer.RandomHighPort()
		if err != nil {
			return 31874
		}
		return port
	}
	return installer.BuildRURecommendedInstall(installer.RURecommendedInstallInput{
		Domain:          opts.Domain,
		Email:           opts.Email,
		Stack:           installer.Stack(opts.Stack),
		Port:            opts.SharedPort,
		PanelPort:       opts.PanelPort,
		Availability:    availability,
		Secret:          randomSecret,
		RandomPort:      randomPort,
		RandomPanelPort: installer.RandomHighPort,
	})
}
