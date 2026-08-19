package privileged

import (
	"encoding/json"
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

func TestSupportedOperationContract(t *testing.T) {
	operations := []Operation{
		OperationPromote,
		OperationServiceAction,
		OperationServiceStatus,
		OperationJournal,
		OperationBackupCreate,
		OperationBackupList,
		OperationBackupVerify,
		OperationBackupRead,
		OperationBackupPrune,
		OperationBackupRestore,
		OperationBackupDelete,
		OperationRotateKey,
		OperationRecoverKeyRotation,
		OperationFirewallApply,
		OperationStageUpdate,
		OperationRestartPanel,
	}
	for _, operation := range operations {
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

func TestPayloadMatchesOperationForAllOperations(t *testing.T) {
	cases := []struct {
		op        Operation
		payload   func(*RequestEnvelope)
		wantMatch bool
	}{
		{OperationPromote, func(r *RequestEnvelope) { r.Promote = &PromoteRequest{} }, true},
		{OperationPromote, func(r *RequestEnvelope) { r.RestartPanel = &RestartPanelRequest{} }, false},
		{OperationServiceAction, func(r *RequestEnvelope) { r.ServiceAction = &ServiceActionRequest{} }, true},
		{OperationServiceStatus, func(r *RequestEnvelope) { r.ServiceStatus = &ServiceStatusRequest{} }, true},
		{OperationJournal, func(r *RequestEnvelope) { r.Journal = &JournalRequest{} }, true},
		{OperationBackupCreate, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupList, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupVerify, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupRead, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupPrune, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupRestore, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationBackupDelete, func(r *RequestEnvelope) { r.Backup = &BackupRequest{} }, true},
		{OperationRotateKey, func(r *RequestEnvelope) { r.RotateKey = &RotateKeyRequest{} }, true},
		{OperationRecoverKeyRotation, func(r *RequestEnvelope) { r.RecoverKeyRotation = &RecoverKeyRotationRequest{} }, true},
		{OperationFirewallApply, func(r *RequestEnvelope) { r.Firewall = &FirewallRequest{} }, true},
		{OperationStageUpdate, func(r *RequestEnvelope) { r.Update = &UpdateRequest{} }, true},
		{OperationRestartPanel, func(r *RequestEnvelope) { r.RestartPanel = &RestartPanelRequest{} }, true},
		{OperationSyncCaddyCert, func(r *RequestEnvelope) { r.SyncCaddyCert = &SyncCaddyCertRequest{} }, true},
	}
	for _, tc := range cases {
		r := RequestEnvelope{Version: ProtocolVersion, RequestID: "x", Operation: tc.op}
		tc.payload(&r)
		if got := r.payloadMatchesOperation(); got != tc.wantMatch {
			t.Fatalf("payloadMatchesOperation(%q) = %v, want %v", tc.op, got, tc.wantMatch)
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
