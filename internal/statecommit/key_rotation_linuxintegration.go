//go:build linuxintegration

package statecommit

import "fmt"

// RotateKeyInterruptedForIntegration executes the production rotation protocol
// but returns immediately after the named durable phase. It is compiled only
// into linuxintegration test binaries; production builds cannot request an
// artificial interruption.
func RotateKeyInterruptedForIntegration(options RotateKeyOptions, phase string) (KeyRotationResult, error) {
	switch keyRotationPhase(phase) {
	case rotationPhasePrepared, rotationPhaseKeyPublished, rotationPhaseStatePublished, rotationPhaseSQLiteCommitted:
		options.interruptAfter = keyRotationPhase(phase)
	default:
		return KeyRotationResult{}, fmt.Errorf("unknown integration key-rotation phase %q", phase)
	}
	return RotateKey(options)
}
