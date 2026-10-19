import argparse
import json
from pathlib import Path
import runpy
import subprocess
import tempfile
import unittest
from unittest.mock import patch

PROJECT = Path(__file__).resolve().parent.parent
MODULE = runpy.run_path(str(PROJECT / "scripts/qualify-model.py"))


def report(fail=False):
    names = ("valid_output_schema", "conventional_commit", "allowed_scope", "subject_length", "unsupported_component", "unsupported_issue_identifier", "unsupported_test_outcome_claim", "unsupported_behavioral_claim", "daemon_grounding_coverage")
    summary = {name: {"checked": 3, "passed": 3} for name in names}
    summary.update(cases=1, candidate_count=3, timeout_rate=0, error_rate=int(fail), warm_p95_ms=100)
    return {"summary": summary, "provider": {"name": "ollama", "model": "fixture:tag", "model_digest": "a" * 64, "prompt_version": "v2"}, "corpus_digest": "b" * 64}


class Qualification(unittest.TestCase):
    def test_development_failure_never_runs_held_out(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result_path = root / "result.json"
            result_path.write_text(json.dumps(report(fail=True)))
            args = argparse.Namespace(model="fixture:tag", context_tokens=4096, output_tokens=256, warm_timeout=8, cold_timeout=30, keep_alive=300, output_dir=root / "qualification", binary=root / "binary", corpus="v3")
            completed = subprocess.CompletedProcess([], 0, json.dumps({"reports": {"json": str(result_path)}}))
            with patch("subprocess.run", return_value=completed) as run:
                result = MODULE["run_qualification"](args)
            self.assertEqual(run.call_count, 1)
            self.assertEqual(result["status"], "rejected_by_operational_gate")
            self.assertTrue(result["human_review_required"])
            self.assertEqual((args.output_dir / "qualification.json").stat().st_mode & 0o777, 0o600)

    def test_success_requires_three_held_out_runs_and_human_review(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result_path = root / "result.json"
            result_path.write_text(json.dumps(report()))
            args = argparse.Namespace(model="fixture:tag", context_tokens=4096, output_tokens=256, warm_timeout=8, cold_timeout=30, keep_alive=300, output_dir=root / "qualification", binary=root / "binary", corpus="v3")
            completed = subprocess.CompletedProcess([], 0, json.dumps({"reports": {"json": str(result_path)}}))
            with patch("subprocess.run", return_value=completed) as run:
                result = MODULE["run_qualification"](args)
            self.assertEqual(run.call_count, 4)
            self.assertEqual(result["status"], "operational_checks_passed_human_review_required")
            self.assertTrue(result["human_review_required"])
            self.assertIn('fallback = "none"', (args.output_dir / "config.toml").read_text())

    def test_missing_binary_records_terminal_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = argparse.Namespace(model="fixture:tag", context_tokens=4096, output_tokens=256, warm_timeout=8, cold_timeout=30, keep_alive=300, output_dir=root / "qualification", binary=root / "missing", corpus="v3")
            with self.assertRaises(OSError):
                MODULE["run_qualification"](args)
            result = json.loads((args.output_dir / "qualification.json").read_text())
            self.assertEqual(result["status"], "runtime_failure")


if __name__ == "__main__":
    unittest.main()
