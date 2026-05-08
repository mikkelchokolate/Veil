package cli

import "fmt"

type RURecommendedInstallRequirements struct{}

func NewRURecommendedInstallRequirements() RURecommendedInstallRequirements {
	return RURecommendedInstallRequirements{}
}

func (r RURecommendedInstallRequirements) Validate(opts ruRecommendedInstallOptions) error {
	if opts.PanelAccess == "caddy" && (opts.Domain == "" || opts.Email == "") {
		return fmt.Errorf("--domain and --email are required for caddy Panel access")
	}
	return nil
}
