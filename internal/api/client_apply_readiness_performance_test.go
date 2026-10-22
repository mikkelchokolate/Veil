package api

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func runtimeFingerprintFixture(count int) model.ManagementSnapshot {
	var snapshot model.ManagementSnapshot
	expires, quota := int64(12345), int64(54321)
	for i := range count {
		id := fmt.Sprintf("client-%d", i)
		snapshot.Clients = append(snapshot.Clients, model.ClientSnapshot{
			ID: id, Name: id, Enabled: i%2 == 0, ExpiresAt: &expires,
			QuotaBytes: &quota, QuotaResetPolicy: "daily", Depleted: i%3 == 0,
		})
		for j := range 2 {
			bindingID := fmt.Sprintf("%s-binding-%d", id, j)
			snapshot.Bindings = append(snapshot.Bindings, model.BindingSnapshot{
				ID: bindingID, ClientID: id, InboundID: fmt.Sprintf("inbound-%d", j),
				RuntimeIdentity: bindingID, Enabled: j == 0,
			})
			for version := range 2 {
				snapshot.Credentials = append(snapshot.Credentials, model.CredentialSnapshot{
					BindingID: bindingID, Kind: "password", CredentialVersion: version + 1,
					EncryptedValue: []byte(fmt.Sprintf("encrypted-%s-%d", bindingID, version)),
				})
			}
		}
	}
	return snapshot
}

func TestBatchedClientFingerprintsPreserveMaterial(t *testing.T) {
	snapshot := runtimeFingerprintFixture(30)
	snapshot.Clients = append(snapshot.Clients, model.ClientSnapshot{ID: "orphan"})
	snapshot.Bindings = append(snapshot.Bindings, model.BindingSnapshot{ID: "unowned", ClientID: "missing"})
	snapshot.Credentials = append(snapshot.Credentials, model.CredentialSnapshot{BindingID: "unowned", Kind: "password"})
	want := make(map[string]string, len(snapshot.Clients))
	for _, current := range snapshot.Clients {
		want[current.ID] = clientRuntimeFingerprint(snapshot, current.ID)
	}
	if got := clientRuntimeFingerprints(snapshot); !reflect.DeepEqual(got, want) {
		t.Fatal("batched fingerprints differ from per-client fingerprints")
	}
	for i, j := 0, len(snapshot.Bindings)-1; i < j; i, j = i+1, j-1 {
		snapshot.Bindings[i], snapshot.Bindings[j] = snapshot.Bindings[j], snapshot.Bindings[i]
	}
	for i, j := 0, len(snapshot.Credentials)-1; i < j; i, j = i+1, j-1 {
		snapshot.Credentials[i], snapshot.Credentials[j] = snapshot.Credentials[j], snapshot.Credentials[i]
	}
	if got := clientRuntimeFingerprints(snapshot); !reflect.DeepEqual(got, want) {
		t.Fatal("fingerprints depend on repository row order")
	}
	if got := clientRuntimeFingerprints(model.ManagementSnapshot{}); len(got) != 0 {
		t.Fatal("empty snapshot has fingerprints")
	}
}

func BenchmarkClientRuntimeFingerprints(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		snapshot := runtimeFingerprintFixture(count)
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.Run("batched", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					clientRuntimeFingerprints(snapshot)
				}
			})
			b.Run("previous", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					for _, current := range snapshot.Clients {
						clientRuntimeFingerprint(snapshot, current.ID)
					}
				}
			})
		})
	}
}
