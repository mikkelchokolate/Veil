package cli

import "github.com/veil-panel/veil/internal/installer"

func buildRepairPlanFromOptions(opts repairWorkflowOptions) (installer.RepairPlan, error) {
	built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Domain:       opts.Domain,
		Email:        opts.Email,
		Stack:        installer.StackPanel,
		Port:         0,
		Availability: installer.PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       randomSecret,
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	return installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir})
}
