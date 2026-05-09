package runtime

type ConnectionProcSocketPath struct{}

func NewConnectionProcSocketPath() ConnectionProcSocketPath { return ConnectionProcSocketPath{} }

func (ConnectionProcSocketPath) ForProtocol(proto string) string {
	return "/proc/net/" + proto
}
