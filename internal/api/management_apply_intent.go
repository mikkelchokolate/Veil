package api

type ManagementApplyIntentInput struct {
	ApplyRoot string
	// LiveRoot is the production live generated root; threaded into both the
	// render check and the plan preview so custom installs display and target
	// the same destination (issue #636).
	LiveRoot        string
	Settings        Settings
	Inbounds        []Inbound
	Rules           []RoutingRule
	RoutingSource   RoutingSource
	Warp            WarpConfig
	SkipRenderCheck bool
}

type ManagementApplyIntent struct {
	input ManagementApplyIntentInput
}

func NewManagementApplyIntent(input ManagementApplyIntentInput) ManagementApplyIntent {
	return ManagementApplyIntent{input: input}
}

func (i ManagementApplyIntent) BuildPlan() ApplyPlanResponse {
	input := i.input
	renderer := NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: input.ApplyRoot,
		LiveRoot:  input.LiveRoot,
		Settings:  input.Settings,
		Inbounds:  input.Inbounds,
		Rules:     input.Rules,
		Warp:      input.Warp,
	})
	planInput := ApplyPlanInput{
		ApplyRoot:               input.ApplyRoot,
		LiveRoot:                input.LiveRoot,
		Settings:                input.Settings,
		Inbounds:                input.Inbounds,
		Rules:                   input.Rules,
		RoutingSource:           input.RoutingSource,
		Warp:                    input.Warp,
		RenderSettingsAvailable: renderer.HasRenderSettings(),
	}
	if !input.SkipRenderCheck {
		planInput.ValidateInboundRender = func(inbound Inbound) error {
			_, err := renderer.RenderInbound(inbound)
			return err
		}
		planInput.ValidateWarpRender = func() error {
			_, err := renderer.RenderWarp()
			return err
		}
	}
	return BuildApplyPlan(planInput)
}
