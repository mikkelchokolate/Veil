package repair

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
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

// #1288: the protocol-unit DNS warning is printed after the plan on both
// dry-run and apply, and nothing is printed when the probe is clean.
func TestRunPrintsEgressDNSWarning(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		warning string
	}{
		{"dry-run warns", true, "no resolv.conf nameserver is reachable (#1288)"},
		{"apply warns", false, "no resolv.conf nameserver is reachable (#1288)"},
		{"clean probe", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := Run(Options{Profile: "ru-recommended", DryRun: tc.dryRun, Yes: !tc.dryRun}, &out, Dependencies{
				BuildPlan:        func(Options) (installer.RepairPlan, error) { return installer.RepairPlan{}, nil },
				ApplyPlan:        func(installer.RepairPlan, Options) error { return nil },
				EgressDNSWarning: func() string { return tc.warning },
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := out.String()
			if tc.warning == "" {
				if strings.Contains(got, "warning:") {
					t.Fatalf("clean probe printed a warning:\n%s", got)
				}
				return
			}
			planAt := strings.Index(got, "Veil repair plan")
			warnAt := strings.Index(got, "warning: "+tc.warning)
			if planAt < 0 || warnAt < planAt {
				t.Fatalf("warning must follow the plan:\n%s", got)
			}
		})
	}
}

// #1500: plan-building findings (a failed certificate renewal, an
// unreproducible Caddy configuration) are printed with the plan on both dry-run
// and apply, so a successful exit never hides them.
func TestRunPrintsPlanBuildWarnings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		warning string
	}{
		{"dry-run warns", true, "could not renew the Let's Encrypt IP certificate: permission denied"},
		{"apply warns", false, "could not renew the Let's Encrypt IP certificate: permission denied"},
		{"no warning", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			warnings := []string{}
			if tc.warning != "" {
				warnings = append(warnings, tc.warning)
			}
			err := Run(Options{Profile: "ru-recommended", DryRun: tc.dryRun, Yes: !tc.dryRun}, &out, Dependencies{
				BuildPlan: func(Options) (installer.RepairPlan, error) { return installer.RepairPlan{}, nil },
				ApplyPlan: func(installer.RepairPlan, Options) error { return nil },
				Warnings:  func() []string { return warnings },
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := out.String()
			if tc.warning == "" {
				if strings.Contains(got, "warning:") {
					t.Fatalf("no warnings but output has one:\n%s", got)
				}
				return
			}
			planAt := strings.Index(got, "Veil repair plan")
			warnAt := strings.Index(got, "warning: "+tc.warning)
			if planAt < 0 || warnAt < planAt {
				t.Fatalf("warning must follow the plan:\n%s", got)
			}
		})
	}
}
