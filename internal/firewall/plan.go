package firewall

import "fmt"

type Config struct {
	SharedPort int
	PanelPort  int
}

type Rule struct {
	Command string
	Args    []string
}

func UFWPlan(config Config) []Rule {
	if config.SharedPort <= 0 {
		return nil
	}
	rules := []Rule{
		{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.SharedPort), "comment", "Veil NaiveProxy"}},
		{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/udp", config.SharedPort), "comment", "Veil Hysteria2"}},
	}
	if config.PanelPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelPort), "comment", "Veil panel"}})
	}
	return rules
}
