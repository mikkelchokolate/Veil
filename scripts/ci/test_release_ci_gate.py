import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("release_ci_gate", Path(__file__).with_name("release-ci-gate.py"))
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)
SHA = "a" * 40


def run(filename="ci.yml", identity=1, number=1, attempt=1, status="completed", conclusion="success"):
    return {
        "id": number * 10 + identity, "run_number": number, "run_attempt": attempt,
        "workflow_id": identity, "path": f".github/workflows/{filename}",
        "event": "push", "head_branch": "main", "head_sha": SHA,
        "repository": {"full_name": gate.REPOSITORY},
        "head_repository": {"full_name": gate.REPOSITORY},
        "status": status, "conclusion": conclusion,
    }


def listing(*runs):
    return [{"total_count": len(runs), "workflow_runs": list(runs)}]


def jobs(filename, selected):
    return [{
        "id": index, "name": name, "run_id": selected["id"], "run_attempt": selected["run_attempt"],
        "head_sha": SHA, "status": "completed", "conclusion": "success",
    } for index, name in enumerate(sorted(gate.REQUIRED_JOBS[filename]), 1)]


def job_listing(filename, selected):
    rows = jobs(filename, selected)
    return [{"total_count": len(rows), "jobs": rows}]


class ReleaseCIGateTests(unittest.TestCase):
    def setUp(self):
        self.output = patch("sys.stdout", new_callable=io.StringIO).start()
        self.addCleanup(patch.stopall)

    def verify_api(self, ci_runs=None, ci_current=None):
        ci = run()
        codeql = run("codeql.yml", 2)
        responses = [
            {"id": 1, "path": ".github/workflows/ci.yml", "state": "active"},
            {"id": 2, "path": ".github/workflows/codeql.yml", "state": "active"},
            listing(*(ci_runs if ci_runs is not None else [ci])), ci_current or ci,
            job_listing("ci.yml", ci_current or ci),
            listing(*(ci_runs if ci_runs is not None else [ci])), ci_current or ci,
            listing(codeql), codeql, job_listing("codeql.yml", codeql), listing(codeql), codeql,
        ]
        with patch.object(gate, "api", side_effect=responses) as api:
            chosen = gate.verify(gate.REPOSITORY, SHA)
        return chosen, api

    def test_exact_trusted_runs_succeed_and_emit_provenance_links(self):
        chosen, api = self.verify_api()
        self.assertEqual(chosen, {"ci.yml": 11, "codeql.yml": 12})
        self.assertIn("/actions/runs/11/attempts/1", self.output.getvalue())
        self.assertIn(f"source={SHA}", self.output.getvalue())
        self.assertEqual(api.call_args_list[2].args[2][2], ("head_sha", SHA))

    def test_latest_failed_run_never_falls_back_to_older_success(self):
        old = run(number=1)
        failed = run(number=2, conclusion="failure")
        with self.assertRaisesRegex(gate.GateError, "latest ci.yml attempt did not succeed"):
            self.verify_api([old, failed], failed)

    def test_failed_rerun_invalidates_green_listed_attempt(self):
        with self.assertRaisesRegex(gate.GateError, "failure"):
            self.verify_api(ci_current=run(attempt=2, conclusion="failure"))

    def test_every_unsuccessful_conclusion_is_rejected(self):
        for conclusion in ("failure", "cancelled", "skipped", "timed_out", "stale", "action_required", "neutral"):
            with self.subTest(conclusion=conclusion), self.assertRaises(gate.GateError):
                self.verify_api(ci_current=run(conclusion=conclusion))

    def test_wrong_source_workflow_event_branch_or_repository_is_rejected(self):
        mutations = {
            "head_sha": "b" * 40, "workflow_id": 99, "path": ".github/workflows/spoof.yml",
            "event": "pull_request", "head_branch": "feature",
            "repository": {"full_name": "fork/Veil"}, "head_repository": {"full_name": "fork/Veil"},
        }
        for field, value in mutations.items():
            candidate = run()
            candidate[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(gate.GateError, "untrusted"):
                self.verify_api(ci_current=candidate)

    def test_untrusted_repository_and_invalid_sha_fail_before_api(self):
        with patch.object(gate, "api") as api:
            for repository, sha in (("fork/Veil", SHA), (gate.REPOSITORY, "main"), (gate.REPOSITORY, SHA.upper())):
                with self.subTest(repository=repository, sha=sha), self.assertRaises(gate.GateError):
                    gate.verify(repository, sha)
            api.assert_not_called()

    def test_wrong_or_disabled_workflow_metadata_fails(self):
        for metadata in ({"id": 1, "path": ".github/workflows/fake.yml", "state": "active"},
                         {"id": 1, "path": ".github/workflows/ci.yml", "state": "disabled_manually"},
                         {"id": True, "path": ".github/workflows/ci.yml", "state": "active"}):
            with patch.object(gate, "api", return_value=metadata), self.assertRaises(gate.GateError):
                gate.verify(gate.REPOSITORY, SHA)

    def test_listing_validates_every_row_not_only_selected(self):
        spoof = run(number=1)
        spoof["head_repository"] = {"full_name": "fork/Veil"}
        with self.assertRaisesRegex(gate.GateError, "head_repository"):
            self.verify_api([spoof, run(number=2)])

    def test_malformed_duplicate_or_incomplete_listing_fails(self):
        for response in ({}, [], [{"workflow_runs": []}], listing(run(), run()),
                         [{"total_count": 2, "workflow_runs": [run()]}]):
            with patch.object(gate, "api", return_value=response), self.subTest(response=response), self.assertRaises(gate.GateError):
                gate.latest_run("ci.yml", 1, SHA, 1200)

    def test_stale_attempt_and_mismatched_run_are_rejected(self):
        for current in (run(attempt=1), run(number=2)):
            with patch.object(gate, "api", side_effect=[listing(run(attempt=2)), current]), self.assertRaises(gate.GateError):
                gate.latest_run("ci.yml", 1, SHA, 1200)

    def test_malformed_run_fields_are_rejected(self):
        for field, value in (("id", True), ("run_number", None), ("run_attempt", 0),
                             ("status", "unknown"), ("status", []), ("conclusion", None)):
            candidate = run()
            candidate[field] = value
            with self.subTest(field=field), self.assertRaises(gate.GateError):
                gate.validate_run(candidate, "ci.yml", 1, SHA)
        with self.assertRaises(gate.GateError):
            gate.validate_run(run(status="queued", conclusion="success"), "ci.yml", 1, SHA)

    def test_absent_and_pending_runs_wait_then_pass(self):
        ci, codeql = run(), run("codeql.yml", 2)
        with patch.object(gate, "workflow_id", side_effect=[1, 2]), \
                patch.object(gate, "latest_run", side_effect=[None, run("codeql.yml", 2, status="queued", conclusion=None), ci, ci, codeql, codeql]), \
                patch.object(gate, "verify_jobs"), \
                patch.object(gate.time, "sleep") as sleep:
            self.assertEqual(gate.verify(gate.REPOSITORY, SHA), {"ci.yml": 11, "codeql.yml": 12})
            sleep.assert_called_once_with(30)

    def test_overall_success_cannot_hide_missing_or_skipped_jobs(self):
        selected = run()
        valid = jobs("ci.yml", selected)
        for rows in (valid[:-1], [dict(row, conclusion="skipped") if row["name"] == "test" else row for row in valid],
                     [dict(row, conclusion="neutral") if row["name"] == "test" else row for row in valid]):
            with patch.object(gate, "api", return_value=[{"total_count": len(rows), "jobs": rows}]), self.assertRaises(gate.GateError):
                gate.verify_jobs("ci.yml", selected, SHA, 1200)

    def test_jobs_must_belong_to_exact_run_attempt_and_sha(self):
        selected = run(attempt=2)
        for field, value in (("run_id", 999), ("run_attempt", 1), ("head_sha", "b" * 40), ("status", "in_progress")):
            rows = jobs("ci.yml", selected)
            rows[0][field] = value
            with self.subTest(field=field), patch.object(gate, "api", return_value=[{"total_count": len(rows), "jobs": rows}]), self.assertRaises(gate.GateError):
                gate.verify_jobs("ci.yml", selected, SHA, 1200)

    def test_jobs_pagination_and_extra_job_must_all_succeed(self):
        selected = run()
        rows = jobs("ci.yml", selected)
        pages = [{"total_count": len(rows), "jobs": rows[:5]}, {"total_count": len(rows), "jobs": rows[5:]}]
        with patch.object(gate, "api", return_value=pages) as api:
            gate.verify_jobs("ci.yml", selected, SHA, 1200)
            self.assertIn("/attempts/1/jobs", api.call_args.args[0])
            self.assertTrue(api.call_args.kwargs["paginate"])
        rows.append(dict(rows[0], id=99, name="extra", conclusion="cancelled"))
        with patch.object(gate, "api", return_value=[{"total_count": len(rows), "jobs": rows}]), self.assertRaises(gate.GateError):
            gate.verify_jobs("ci.yml", selected, SHA, 1200)

    def test_codeql_requires_all_three_analyses(self):
        selected = run("codeql.yml", 2)
        rows = jobs("codeql.yml", selected)[:-1]
        with patch.object(gate, "api", return_value=[{"total_count": len(rows), "jobs": rows}]), self.assertRaisesRegex(gate.GateError, "missing required jobs"):
            gate.verify_jobs("codeql.yml", selected, SHA, 1200)

    def test_ephemeral_run_metadata_does_not_invalidate_verified_evidence(self):
        ci, codeql = run(), run("codeql.yml", 2)
        fresh_ci = dict(ci, repository={"full_name": gate.REPOSITORY, "open_issues_count": 100})
        fresh_codeql = dict(codeql, updated_at="new unrelated metadata", check_suite_url="another URL")
        with patch.object(gate, "workflow_id", side_effect=[1, 2]), \
                patch.object(gate, "latest_run", side_effect=[ci, fresh_ci, codeql, fresh_codeql]), \
                patch.object(gate, "verify_jobs"), patch.object(gate.time, "sleep") as sleep:
            self.assertEqual(gate.verify(gate.REPOSITORY, SHA), {"ci.yml": 11, "codeql.yml": 12})
            sleep.assert_not_called()

    def test_changed_attempt_requires_new_job_evidence_and_never_accepts_old_green(self):
        ci, codeql = run(), run("codeql.yml", 2)
        newer = run(attempt=2, conclusion="failure")
        with patch.object(gate, "workflow_id", side_effect=[1, 2]), \
                patch.object(gate, "latest_run", side_effect=[ci, newer, codeql, codeql, newer]), \
                patch.object(gate, "verify_jobs"), patch.object(gate.time, "sleep") as sleep, \
                self.assertRaisesRegex(gate.GateError, "latest ci.yml attempt did not succeed: failure"):
            gate.verify(gate.REPOSITORY, SHA)
        sleep.assert_called_once_with(30)

    def test_duplicate_or_malformed_job_evidence_is_rejected(self):
        selected = run()
        valid = jobs("ci.yml", selected)
        for rows in (valid + [valid[0]], [dict(valid[0], id=True)] + valid[1:],
                     [dict(valid[0], run_attempt=True)] + valid[1:], [None] + valid[1:]):
            with self.subTest(rows=rows), patch.object(gate, "api", return_value=[{"total_count": len(rows), "jobs": rows}]), self.assertRaises(gate.GateError):
                gate.verify_jobs("ci.yml", selected, SHA, 1200)

    def test_wait_expires_at_twenty_minutes(self):
        clock = [0]

        def sleep(seconds):
            clock[0] += seconds

        with patch.object(gate.time, "monotonic", side_effect=lambda: clock[0]), \
                patch.object(gate.time, "sleep", side_effect=sleep), \
                patch.object(gate, "workflow_id", side_effect=[1, 2]), \
                patch.object(gate, "latest_run", return_value=None), \
                self.assertRaisesRegex(gate.GateError, "after 20 minutes"):
            gate.verify(gate.REPOSITORY, SHA)
        self.assertEqual(clock[0], 1200)

    def test_api_uses_authenticated_official_gh_without_shell(self):
        result = subprocess.CompletedProcess([], 0, stdout='{"ok":true}', stderr="")
        with patch.object(gate.time, "monotonic", return_value=0), \
                patch.object(gate.subprocess, "run", return_value=result) as execute:
            self.assertEqual(gate.api("repos/example", 1200, (("head_sha", SHA),), True), {"ok": True})
        command = execute.call_args.args[0]
        self.assertEqual(command[:6], ["gh", "api", "--hostname", "github.com", "--method", "GET"])
        self.assertIn("--paginate", command)
        self.assertIn("--slurp", command)
        self.assertNotIn("shell", execute.call_args.kwargs)
        self.assertEqual(execute.call_args.kwargs["timeout"], 60)

    def test_api_errors_and_malformed_json_fail_closed(self):
        for result in (subprocess.CompletedProcess([], 1, stdout="", stderr="HTTP403"),
                       subprocess.CompletedProcess([], 0, stdout="not json", stderr="")):
            with patch.object(gate.time, "monotonic", return_value=0), \
                    patch.object(gate.subprocess, "run", return_value=result), self.assertRaises(gate.GateError):
                gate.api("endpoint", 1200)
        for error in (FileNotFoundError("gh"), subprocess.TimeoutExpired("gh", 60)):
            with patch.object(gate.time, "monotonic", return_value=0), \
                    patch.object(gate.subprocess, "run", side_effect=error), self.assertRaises(gate.GateError):
                gate.api("endpoint", 1200)

    def test_main_outputs_ids_only_after_both_gates_succeed(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "output"
            with patch.dict(os.environ, {"GITHUB_OUTPUT": str(output)}), \
                    patch.object(sys, "argv", ["gate", "--repository", gate.REPOSITORY, "--sha", SHA]), \
                    patch.object(gate, "verify", return_value={"ci.yml": 11, "codeql.yml": 12}):
                self.assertEqual(gate.main(), 0)
            self.assertEqual(output.read_text(), "ci_run_id=11\ncodeql_run_id=12\n")
            with patch.dict(os.environ, {"GITHUB_OUTPUT": str(output)}), \
                    patch.object(sys, "argv", ["gate", "--repository", gate.REPOSITORY, "--sha", SHA]), \
                    patch.object(gate, "verify", side_effect=gate.GateError("failed")), patch("sys.stderr", new_callable=io.StringIO):
                self.assertEqual(gate.main(), 1)
            self.assertEqual(output.read_text(), "ci_run_id=11\ncodeql_run_id=12\n")


if __name__ == "__main__":
    unittest.main()
