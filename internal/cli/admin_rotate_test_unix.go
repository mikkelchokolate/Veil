//go:build !windows

package cli

func isFailureSimulationSupported() bool {
	return false
}

func lockStateFileForRenameFailure(path string) (func(), error) {
	return func() {}, nil
}
