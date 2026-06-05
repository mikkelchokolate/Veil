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
		OperationRotateKey,
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
