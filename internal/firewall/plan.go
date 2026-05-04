package firewall

import "fmt"

type Config struct {
	SharedPort int
	PanelPort  int
	EnableTCP  bool
	EnableUDP  bool
}

type Rule struct {
	Command string
	Args    []string
}

func UFWPlan(config Config) []Rule {
	if config.SharedPort <= 0 && config.PanelPort <= 0 {
		return nil
	}
	if config.SharedPort > 0 {
		if !config.EnableTCP && !config.EnableUDP {
			config.EnableTCP = true
			config.EnableUDP = true
		}
	}
	rules := []Rule{}
	if config.SharedPort > 0 && config.EnableTCP {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.SharedPort), "comment", "Veil NaiveProxy"}})
	}
	if config.SharedPort > 0 && config.EnableUDP {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/udp", config.SharedPort), "comment", "Veil Hysteria2"}})
	}
	if config.PanelPort > 0 {
		rules = append(rules, Rule{Command: "ufw", Args: []string{"allow", fmt.Sprintf("%d/tcp", config.PanelPort), "comment", "Veil panel"}})
	}
	return rules
}
