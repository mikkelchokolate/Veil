package privileged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func setRegressionFenceToken(t *testing.T, request any, owner string, generation uint64) {
	t.Helper()
	value := reflect.ValueOf(request)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		t.Fatalf("request must be a non-nil pointer, got %T", request)
	}
	value = value.Elem()
	fence := value.FieldByName("Fence")
	if !fence.IsValid() {
		t.Errorf("%T has no Fence token; privileged runtime mutations are unfenced", request)
		return
	}
	if fence.Kind() == reflect.Pointer {
		fence.Set(reflect.New(fence.Type().Elem()))
		fence = fence.Elem()
	}
	ownerField := fence.FieldByName("Owner")
	generationField := fence.FieldByName("Generation")
	if !ownerField.IsValid() || !ownerField.CanSet() || ownerField.Kind() != reflect.String {
		t.Fatalf("%T Fence.Owner is unavailable", request)
	}
	if !generationField.IsValid() || !generationField.CanSet() || generationField.Kind() != reflect.Uint64 {
		t.Fatalf("%T Fence.Generation is unavailable", request)
	}
	ownerField.SetString(owner)
	generationField.SetUint(generation)
}

func assertStaleFenceRejected(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("stale fencing generation was accepted")
	}
	var operationError *Error
	if !errors.As(err, &operationError) {
		t.Fatalf("stale fence returned untyped error %T: %v", err, err)
	}
	if operationError.Code != ErrorConflict && operationError.Code != ErrorForbiddenOperation {
		t.Fatalf("stale fence error code = %s, want conflict or forbidden_operation", operationError.Code)
	}
}

func runCaddyLoadFenceRegression(t *testing.T, adapter *LocalAdapter, generation uint64) error {
	t.Helper()
	method := reflect.ValueOf(adapter).MethodByName("CaddyLoad")
	if !method.IsValid() {
		t.Errorf("LocalAdapter has no privileged CaddyLoad operation")
		return errors.New("privileged CaddyLoad operation is unavailable")
	}
	methodType := method.Type()
	if methodType.NumIn() != 2 || methodType.NumOut() != 1 {
		t.Fatalf("unexpected CaddyLoad signature: %s", methodType)
	}
	request := reflect.New(methodType.In(1))
	config := request.Elem().FieldByName("Config")
	if config.IsValid() && config.CanSet() && config.Kind() == reflect.Slice {
		config.SetBytes([]byte(`{"apps":{}}`))
	}
	setRegressionFenceToken(t, request.Interface(), "apply-owner", generation)
	result := method.Call([]reflect.Value{reflect.ValueOf(context.Background()), request.Elem()})
	if result[0].IsNil() {
		return nil
	}
	return result[0].Interface().(error)
}

func TestPrivilegedRuntimeMutationsRejectOlderFencingGeneration(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, adapter *LocalAdapter, generation uint64) error
	}{
		{
			name: "promote",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := PromoteRequest{ArtifactIDs: []string{"mieru"}}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Promote(context.Background(), request)
				return err
			},
		},
		{
			name: "rollback-promotion",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := PromoteRequest{RestoreBackupID: "20260729T120000.000000000Z"}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Promote(context.Background(), request)
				return err
			},
		},
		{
			name: "firewall",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.FirewallApply(context.Background(), request)
				return err
			},
		},
		{
			name: "systemd-enable",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := ServiceActionRequest{Unit: "veil-mieru.service", Action: ServiceActionEnable}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				return adapter.ServiceAction(context.Background(), request)
			},
		},
		{
			name: "systemd-disable",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := ServiceActionRequest{Unit: "veil-mieru.service", Action: ServiceActionDisable}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				return adapter.ServiceAction(context.Background(), request)
			},
		},
		{
			name: "systemd-restart",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := ServiceActionRequest{Unit: "veil-mieru.service", Action: ServiceActionRestart}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				return adapter.ServiceAction(context.Background(), request)
			},
		},
		{
			name: "caddy-load",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				return runCaddyLoadFenceRegression(t, adapter, generation)
			},
		},
		{
			name: "certificate-publication",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := SyncCaddyCertRequest{Domain: "veil.invalid", OutDir: t.TempDir()}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.SyncCaddyCert(context.Background(), request)
				return err
			},
		},
		{
			name: "key-rotation",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := RotateKeyRequest{}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				return adapter.RotateKey(context.Background(), request)
			},
		},
		{
			name: "key-rotation-recovery",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := RecoverKeyRotationRequest{}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				return adapter.RecoverKeyRotation(context.Background(), request)
			},
		},
		{
			name: "backup-create",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := BackupRequest{Action: BackupActionCreate}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Backup(context.Background(), request)
				return err
			},
		},
		{
			name: "backup-delete",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := BackupRequest{Action: BackupActionDelete, ArchiveName: "backup-1.enc"}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Backup(context.Background(), request)
				return err
			},
		},
		{
			name: "backup-prune",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := BackupRequest{Action: BackupActionPrune}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Backup(context.Background(), request)
				return err
			},
		},
		{
			name: "backup-restore",
			run: func(t *testing.T, adapter *LocalAdapter, generation uint64) error {
				request := BackupRequest{Action: BackupActionRestore, ArchiveName: "backup-1.enc"}
				setRegressionFenceToken(t, &request, "apply-owner", generation)
				_, err := adapter.Backup(context.Background(), request)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := testPolicy(t)
			staged := filepath.Join(policy.StagingRoot, "mieru", "server_config.json")
			if err := os.WriteFile(staged, []byte("staged"), 0o600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			executor := Executor{
				Promote: func(context.Context, ResolvedPromotion) (PromoteResult, error) {
					calls.Add(1)
					return PromoteResult{}, nil
				},
				ServiceAction: func(context.Context, ServiceActionRequest) error {
					calls.Add(1)
					return nil
				},
				Firewall: func(context.Context, ResolvedFirewall) (FirewallResult, error) {
					calls.Add(1)
					return FirewallResult{}, nil
				},
				SyncCaddyCert: func(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
					calls.Add(1)
					return SyncCaddyCertResult{Found: true}, nil
				},
				RotateKey: func(context.Context, RotateKeyRequest) error {
					calls.Add(1)
					return nil
				},
				RecoverKeyRotation: func(context.Context) error {
					calls.Add(1)
					return nil
				},
				Backup: func(context.Context, ResolvedBackup) (BackupResult, error) {
					calls.Add(1)
					return BackupResult{}, nil
				},
			}
			executorValue := reflect.ValueOf(&executor).Elem()
			if caddyLoad := executorValue.FieldByName("CaddyLoad"); caddyLoad.IsValid() && caddyLoad.CanSet() && caddyLoad.Kind() == reflect.Func {
				caddyLoad.Set(reflect.MakeFunc(caddyLoad.Type(), func([]reflect.Value) []reflect.Value {
					calls.Add(1)
					return []reflect.Value{reflect.Zero(caddyLoad.Type().Out(0))}
				}))
			}
			adapter := NewLocalAdapter(policy, executor)

			if err := test.run(t, adapter, 2); err != nil {
				t.Fatalf("current generation rejected: %v", err)
			}
			assertStaleFenceRejected(t, test.run(t, adapter, 1))
			if got := calls.Load(); got != 1 {
				t.Errorf("executor calls = %d, want exactly the current-generation operation", got)
			}
		})
	}
}

// validRegressionFenceToken builds a complete fencing token; fenceGuard only
// enforces OperationID and LeaseExpiresAt when the policy requires fencing.
func validRegressionFenceToken(owner string, generation uint64) FenceToken {
	return FenceToken{
		Owner:          owner,
		Generation:     generation,
		OperationID:    "apply-operation",
		LeaseExpiresAt: time.Now().UTC().Add(time.Hour).Unix(),
	}
}

// TestNewlyFencedMutationsRequireValidFence locks in the required-fence
// behavior for operations that previously skipped fenceGuard entirely
// (#982 key rotation/recovery) or only fenced restore (#985 backup
// create/delete/prune).
func TestNewlyFencedMutationsRequireValidFence(t *testing.T) {
	tests := []struct {
		name string
		run  func(adapter *LocalAdapter, token FenceToken) error
	}{
		{
			name: "key-rotation",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				return adapter.RotateKey(context.Background(), RotateKeyRequest{Fence: token})
			},
		},
		{
			name: "key-rotation-recovery",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				return adapter.RecoverKeyRotation(context.Background(), RecoverKeyRotationRequest{Fence: token})
			},
		},
		{
			name: "backup-create",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionCreate, Fence: token})
				return err
			},
		},
		{
			name: "backup-delete",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionDelete, ArchiveName: "backup-1.enc", Fence: token})
				return err
			},
		},
		{
			name: "backup-prune",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionPrune, Fence: token})
				return err
			},
		},
		{
			name: "backup-restore",
			run: func(adapter *LocalAdapter, token FenceToken) error {
				_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionRestore, ArchiveName: "backup-1.enc", Fence: token})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := testPolicy(t)
			policy.RequireFence = true
			var calls atomic.Int32
			executor := Executor{
				RotateKey: func(context.Context, RotateKeyRequest) error {
					calls.Add(1)
					return nil
				},
				RecoverKeyRotation: func(context.Context) error {
					calls.Add(1)
					return nil
				},
				Backup: func(context.Context, ResolvedBackup) (BackupResult, error) {
					calls.Add(1)
					return BackupResult{}, nil
				},
			}
			adapter := NewLocalAdapter(policy, executor)

			// Missing token must be rejected before the executor runs.
			assertStaleFenceRejected(t, test.run(adapter, FenceToken{}))
			if got := calls.Load(); got != 0 {
				t.Fatalf("executor ran %d times for an unfenced operation", got)
			}
			// An expired lease must be rejected as well.
			expired := validRegressionFenceToken("apply-owner", 1)
			expired.LeaseExpiresAt = time.Now().UTC().Add(-time.Minute).Unix()
			assertStaleFenceRejected(t, test.run(adapter, expired))
			if got := calls.Load(); got != 0 {
				t.Fatalf("executor ran %d times for an expired-fence operation", got)
			}
			// A current, unexpired token is accepted once.
			if err := test.run(adapter, validRegressionFenceToken("apply-owner", 3)); err != nil {
				t.Fatalf("current fencing token rejected: %v", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("executor calls = %d, want 1", got)
			}
			// A foreign owner replaying the same generation is rejected.
			assertStaleFenceRejected(t, test.run(adapter, validRegressionFenceToken("other-owner", 3)))
			if got := calls.Load(); got != 1 {
				t.Fatalf("executor ran %d times for a foreign-owner fence", got)
			}
		})
	}
}
