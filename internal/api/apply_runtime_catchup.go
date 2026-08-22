package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// catchUpAppliedIfRuntimeUnchangedLocked advances applied_revision when the
// current desired snapshot does not change runtime config relative to the
// already verified applied snapshot. Panel-only writes (locale, password,
// role) still pin an immutable revision so state.json matches SQLite, but they
// must not leave apply state pending forever.
//
// Caller holds s.mu when concurrent with other mutations.
func (s *managementState) catchUpAppliedIfRuntimeUnchangedLocked() error {
	if !s.applyTrackingEnabled() || s.applySnapshots == nil {
		return nil
	}
	rev, err := s.applyRevisions.Get()
	if err != nil {
		return err
	}
	if rev.Desired <= rev.Applied || rev.Applied == 0 {
		return nil
	}
	var status string
	var verified uint64
	if err := s.db.QueryRow(`SELECT status, verified_revision FROM runtime_verification WHERE id=1`).Scan(&status, &verified); err != nil {
		return fmt.Errorf("apply: read runtime verification: %w", err)
	}
	if status != "verified" || verified != rev.Applied {
		return nil
	}
	desired, err := s.loadRevisionSnapshotLocked(rev.Desired)
	if err != nil {
		return err
	}
	applied, err := s.loadRevisionSnapshotLocked(rev.Applied)
	if err != nil {
		return err
	}
	equal, err := runtimeConfigEqual(desired, applied)
	if err != nil {
		return err
	}
	if !equal {
		return nil
	}
	if err := s.applyRevisions.CatchUpApplied(rev.Desired); err != nil {
		return err
	}
	log.Printf("apply: caught up applied revision %d→%d (runtime config unchanged)", rev.Applied, rev.Desired)
	return nil
}

// catchUpAfterPanelMutation converges desired/applied after a users/locale
// write. Those mutations pin a snapshot without auto-apply; catch-up must not
// run inside SaveLocked while the caller still holds s.mu across SQLite I/O
// that can deadlock with the apply runner.
func (s *managementState) catchUpAfterPanelMutation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.catchUpAppliedIfRuntimeUnchangedLocked(); err != nil {
		log.Printf("apply: catch up applied after panel-only mutation: %v", err)
	}
}

func runtimeConfigEqual(a, b managementSnapshot) (bool, error) {
	left, err := json.Marshal(runtimeProjection(a))
	if err != nil {
		return false, err
	}
	right, err := json.Marshal(runtimeProjection(b))
	if err != nil {
		return false, err
	}
	return bytes.Equal(left, right), nil
}

// runtimeProjection keeps the fields that generated live configs depend on and
// drops panel-only state (users/locale/password) plus revision timestamps.
func runtimeProjection(snapshot managementSnapshot) managementSnapshot {
	settings := snapshot.Settings
	if len(settings.ProtocolFields) == 0 {
		settings.ProtocolFields = nil
	}
	inbounds := emptyIfNil(snapshot.Inbounds)
	for i := range inbounds {
		if len(inbounds[i].ProtocolFields) == 0 {
			inbounds[i].ProtocolFields = nil
		}
		if len(inbounds[i].Profiles) == 0 {
			inbounds[i].Profiles = nil
		}
	}
	return managementSnapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Setup:         snapshot.Setup,
		Settings:      settings,
		Inbounds:      inbounds,
		Rules:         emptyIfNil(snapshot.Rules),
		RoutingPreset: snapshot.RoutingPreset,
		RoutingSource: snapshot.RoutingSource,
		Warp: model.WarpConfig{
			Enabled:       snapshot.Warp.Enabled,
			LicenseKey:    snapshot.Warp.LicenseKey,
			Endpoint:      snapshot.Warp.Endpoint,
			PrivateKey:    snapshot.Warp.PrivateKey,
			LocalAddress:  snapshot.Warp.LocalAddress,
			PeerPublicKey: snapshot.Warp.PeerPublicKey,
			Reserved:      emptyIfNil(snapshot.Warp.Reserved),
			SocksListen:   snapshot.Warp.SocksListen,
			SocksPort:     snapshot.Warp.SocksPort,
			MTU:           snapshot.Warp.MTU,
		},
		Clients:     emptyIfNil(snapshot.Clients),
		Bindings:    emptyIfNil(snapshot.Bindings),
		Credentials: emptyIfNil(snapshot.Credentials),
	}
}

func emptyIfNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
