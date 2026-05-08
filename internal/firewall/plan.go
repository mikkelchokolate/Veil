package firewall

import "fmt"

type Config struct {
	PanelPort      int
	PanelHTTPSPort int
}

type Rule struct {
	Command string
	Args    []string
}

func UFWPlan(config Config) []Rule {
	if config.PanelPort <= 0 && config.PanelHTTPSPort <= 0 {
		return nil
	}
	rules := []Rule{}
	if config.PanelPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelPort), "comment", "Veil panel"}})
	}
	if config.PanelHTTPSPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelHTTPSPort), "comment", "Veil panel HTTPS"}})
	}
	return rules
}
