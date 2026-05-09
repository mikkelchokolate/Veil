package repair

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRunDryRunPrintsPlanWithoutApplying(t *testing.T) {
	var out bytes.Buffer
	applied := false
	err := Run(Options{Profile: "ru-recommended", DryRun: true}, &out, Dependencies{
		BuildPlan: func(Options) (installer.RepairPlan, error) {
			return installer.RepairPlan{Actions: []installer.RepairAction{{Path: "/etc/veil/veil.env", Reason: installer.RepairReasonMissing}}}, nil
		},
		ApplyPlan: func(installer.RepairPlan, Options) error { applied = true; return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied {
		t.Fatal("dry run should not apply")
	}
	if !strings.Contains(out.String(), "Veil repair plan") || !strings.Contains(out.String(), "repair missing /etc/veil/veil.env") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestRunAppliesConfirmedPlan(t *testing.T) {
	var out bytes.Buffer
	applied := false
	err := Run(Options{Profile: "ru-recommended", Yes: true}, &out, Dependencies{
		BuildPlan: func(Options) (installer.RepairPlan, error) { return installer.RepairPlan{}, nil },
		ApplyPlan: func(installer.RepairPlan, Options) error { applied = true; return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !applied {
		t.Fatal("expected apply")
	}
}

func TestRunRejectsUnsupportedProfileAndPropagatesPlanError(t *testing.T) {
	if err := Run(Options{Profile: "other"}, &bytes.Buffer{}, Dependencies{}); err == nil || !strings.Contains(err.Error(), "profile \"other\" is not implemented yet") {
		t.Fatalf("profile err = %v", err)
	}
	boom := errors.New("plan failed")
	err := Run(Options{Profile: "ru-recommended"}, &bytes.Buffer{}, Dependencies{BuildPlan: func(Options) (installer.RepairPlan, error) { return installer.RepairPlan{}, boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
