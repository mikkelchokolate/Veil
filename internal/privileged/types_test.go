package privileged

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestRequestContractRoundTrip(t *testing.T) {
	request := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "request-1",
		Operation: OperationServiceAction,
		ServiceAction: &ServiceActionRequest{
			Unit:   "veil-mieru.service",
			Action: ServiceActionRestart,
		},
	}

	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	for _, forbidden := range []string{`"command"`, `"args"`, `"executable"`, `"destination"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("request contract leaked shell or caller-owned path field %s: %s", forbidden, raw)
		}
	}

	var decoded RequestEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("validate round trip: %v", err)
	}
	if decoded.ServiceAction == nil || decoded.ServiceAction.Unit != request.ServiceAction.Unit {
		t.Fatalf("service action payload changed: %+v", decoded.ServiceAction)
	}
}

func TestRequestContractRequiresEnvelopeMetadataAndOnePayload(t *testing.T) {
	valid := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "request-2",
		Operation: OperationJournal,
		Journal:   &JournalRequest{Unit: "veil.service", Lines: 100},
	}
	tests := map[string]RequestEnvelope{
		"missing version": {
			RequestID: "request-2",
			Operation: OperationJournal,
			Journal:   valid.Journal,
		},
		"missing request id": {
			Version:   ProtocolVersion,
			Operation: OperationJournal,
			Journal:   valid.Journal,
		},
		"missing payload": {
			Version:   ProtocolVersion,
			RequestID: "request-2",
			Operation: OperationJournal,
		},
		"multiple payloads": {
			Version:      ProtocolVersion,
			RequestID:    "request-2",
			Operation:    OperationJournal,
			Journal:      valid.Journal,
			RestartPanel: &RestartPanelRequest{},
		},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); err == nil {
				t.Fatal("expected request validation error")
			}
		})
	}
}

func TestRequestContractRejectsUnknownOperationAndPayloadMismatch(t *testing.T) {
	tests := []RequestEnvelope{
		{
			Version:      ProtocolVersion,
			RequestID:    "unknown",
			Operation:    Operation("run_shell"),
			RestartPanel: &RestartPanelRequest{},
		},
		{
			Version:   ProtocolVersion,
			RequestID: "mismatch",
			Operation: OperationJournal,
			Backup:    &BackupRequest{ArchiveName: "daily.enc"},
		},
	}
	for _, request := range tests {
		if err := request.Validate(); err == nil {
			t.Fatalf("expected invalid operation or payload mismatch: %+v", request)
		}
	}
}

// operationConstants scrapes every `OperationX Operation = "..."` constant
// declared in types.go so the contract tests cannot silently miss a newly
// added operation (issue #925). The const block is the authoritative catalog;
// Operation.Valid() must accept each member.
var operationConstantRE = regexp.MustCompile(`Operation[A-Za-z0-9]+\s+Operation\s*=\s*"([^"]+)"`)

func operationConstants(t *testing.T) []Operation {
	t.Helper()
	body, err := os.ReadFile("types.go")
	if err != nil {
		t.Fatal(err)
	}
	var ops []Operation
	for _, match := range operationConstantRE.FindAllStringSubmatch(string(body), -1) {
		ops = append(ops, Operation(match[1]))
	}
	if len(ops) < 10 {
		t.Fatalf("scraped %d operations from types.go — the scrape broke", len(ops))
	}
	return ops
}

// operationPayloadField is the contract between an operation and the
// RequestEnvelope field that must carry its payload. Keyed by the scraped
// operation set, so an unmapped new operation fails the payload test below.
var operationPayloadField = map[Operation]string{
	OperationPromote:            "Promote",
	OperationServiceAction:      "ServiceAction",
	OperationServiceStatus:      "ServiceStatus",
	OperationJournal:            "Journal",
	OperationBackupCreate:       "Backup",
	OperationBackupList:         "Backup",
	OperationBackupVerify:       "Backup",
	OperationBackupRead:         "Backup",
	OperationBackupPrune:        "Backup",
	OperationBackupRestore:      "Backup",
	OperationBackupDelete:       "Backup",
	OperationRotateKey:          "RotateKey",
	OperationRecoverKeyRotation: "RecoverKeyRotation",
	OperationFirewallApply:      "Firewall",
	OperationStageUpdate:        "Update",
	OperationRestartPanel:       "RestartPanel",
	OperationSyncCaddyCert:      "SyncCaddyCert",
	OperationCaddyLoad:          "CaddyLoad",
}

// payloadFieldNames enumerates the RequestEnvelope payload slots by
// reflection: every pointer-to-*Request field is a payload.
func payloadFieldNames(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(RequestEnvelope{})
	var fields []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.Ptr && strings.HasSuffix(f.Type.Elem().Name(), "Request") {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) < 10 {
		t.Fatalf("found %d payload fields on RequestEnvelope — reflection broke", len(fields))
	}
	return fields
}

func TestSupportedOperationContract(t *testing.T) {
	seen := map[Operation]bool{}
	for _, operation := range operationConstants(t) {
		if seen[operation] {
			t.Errorf("operation %q declared twice", operation)
		}
		seen[operation] = true
		if !operation.Valid() {
			t.Errorf("supported operation %q is invalid", operation)
		}
	}
}

func TestBackupReadContractUsesManagedArchiveNameOnly(t *testing.T) {
	request := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "backup-read",
		Operation: OperationBackupRead,
		Backup: &BackupRequest{
			Action:      BackupActionRead,
			ArchiveName: "veil_backup_20260605_120000.tar.gz.enc",
		},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("backup read request rejected: %v", err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"path"`, `"passphrase"`, `"directory"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("backup read contract leaked %s: %s", forbidden, raw)
		}
	}
}

// TestPayloadMatchesOperationForAllOperations exercises the full matrix:
// every declared operation against every payload slot. The operation set is
// scraped from the const block and the payload slots are reflected from
// RequestEnvelope, so adding an operation or a payload field extends the
// matrix automatically instead of drifting (issue #925).
func TestPayloadMatchesOperationForAllOperations(t *testing.T) {
	fields := payloadFieldNames(t)
	for _, op := range operationConstants(t) {
		wantField, ok := operationPayloadField[op]
		if !ok {
			t.Errorf("operation %q has no entry in operationPayloadField", op)
			continue
		}
		mapped := false
		for _, field := range fields {
			r := RequestEnvelope{Version: ProtocolVersion, RequestID: "x", Operation: op}
			slot := reflect.ValueOf(&r).Elem().FieldByName(field)
			slot.Set(reflect.New(slot.Type().Elem()))
			want := field == wantField
			if got := r.payloadMatchesOperation(); got != want {
				t.Errorf("operation %q with payload %s: payloadMatchesOperation=%v, want %v", op, field, got, want)
			}
			if want {
				mapped = true
			}
		}
		if !mapped {
			t.Errorf("operation %q maps to RequestEnvelope.%s which is not a payload field", op, wantField)
		}
	}
}

func TestPayloadMatchesOperationRejectsUnknownOperation(t *testing.T) {
	r := RequestEnvelope{Version: ProtocolVersion, RequestID: "x", Operation: Operation("unknown"), RestartPanel: &RestartPanelRequest{}}
	if r.payloadMatchesOperation() {
		t.Fatal("expected unknown operation to not match any payload")
	}
}

func TestOperationValidRejectsUnknown(t *testing.T) {
	if Operation("nope").Valid() {
		t.Fatal("expected unknown operation to be invalid")
	}
}
