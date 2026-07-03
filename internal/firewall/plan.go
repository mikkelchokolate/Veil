package firewall

import "fmt"

type Config struct {
	PanelAccess    string
	PanelPort      int
	PanelHTTPSPort int
	LEIPCertPort   int
}

type Rule struct {
	Command string
	Args    []string
}

func UFWPlan(config Config) []Rule {
	if config.PanelPort <= 0 && config.PanelHTTPSPort <= 0 && config.LEIPCertPort <= 0 {
		return nil
	}
	rules := []Rule{}
	// The panel port only needs to be opened when the panel is exposed publicly.
	// In local mode the panel listens on loopback, so a firewall rule is useless.
	if config.PanelPort > 0 && config.PanelAccess != "local" {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelPort), "comment", "Veil panel"}})
	}
	if config.PanelHTTPSPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelHTTPSPort), "comment", "Veil panel HTTPS"}})
	}
	if config.LEIPCertPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.LEIPCertPort), "comment", "Veil ACME HTTP-01"}})
	}
	return rules
}
