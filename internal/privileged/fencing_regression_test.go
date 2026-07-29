package privileged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
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
