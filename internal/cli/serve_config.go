package cli

type serveConfig struct {
	Listen          string
	ListenSource    string
	Token           string
	TokenSource     string
	StatePath       string
	StateSource     string
	ApplyRoot       string
	ApplyRootSource string
	KeyPath         string
	KeySource       string
	PanelAccess     string
	Domain          string
	Email           string
	WebBasePath     string
	TLSEnabled      bool
	TLSSource       string
}

func resolveServeConfig(opts serveWorkflowOptions) (serveConfig, error) {
	return NewServeSecurity(opts).Resolve()
}
