package installer

type BuildHint struct {
	BinaryPath string
	Commands   []string
}

func CaddyPanelBuildHint(binaryPath string) BuildHint {
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/caddy"
	}
	return BuildHint{BinaryPath: binaryPath, Commands: []string{"requires standard Caddy at " + binaryPath}}
}
