package mieru

// Plugin implements the Mieru protocol.
type Plugin struct{}

// New creates a Mieru plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "mieru" }
func (Plugin) DisplayName() string     { return "Mieru" }
func (Plugin) Transports() []string    { return []string{"tcp", "udp"} }
func (Plugin) RequiresCaddy() bool     { return false }
func (Plugin) FirewallService() string { return "Veil Mieru" }
func (Plugin) MaxEnabled() int         { return 0 }

// EnforcesPerClientCredentials reports that mita authenticates each client
// with its own username/password pair (audit #309).
func (Plugin) EnforcesPerClientCredentials() bool { return true }
