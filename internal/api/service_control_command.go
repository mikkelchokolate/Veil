package api

type ServiceControlCommand struct {
	units map[string]string
}

func NewServiceControlCommand() ServiceControlCommand {
	return ServiceControlCommand{units: map[string]string{
		"veil":      "veil.service",
		"caddy":     "caddy.service",
		"hysteria2": "hysteria2.service",
		"sing-box":  "sing-box.service",
		"mieru":     "veil-mieru.service",
	}}
}

func (c ServiceControlCommand) Build(name, action string) ([]string, bool) {
	unit, ok := c.units[name]
	if !ok {
		return nil, false
	}
	return []string{"systemctl", action, unit}, true
}

func (c ServiceControlCommand) Allows(name string) bool {
	_, ok := c.units[name]
	return ok
}
