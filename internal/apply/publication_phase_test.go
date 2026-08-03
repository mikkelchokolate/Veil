package apply

import "context"

func markTestRuntimeConverged(ctx context.Context) error {
	for _, phase := range []string{
		PublicationPhaseArtifactsPrepared,
		PublicationPhaseArtifactsCommitted,
		PublicationPhaseServicesPlanned,
		PublicationPhaseServicesConverged,
		PublicationPhaseHealthVerified,
	} {
		if err := AdvanceRuntimePublication(ctx, phase, PublicationDetails{}); err != nil {
			return err
		}
	}
	return nil
}
