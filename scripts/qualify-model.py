#!/usr/bin/env python3
"""Run a local-only development gate, then three held-out operational runs."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess


def gate(report, warm_budget_ms):
    summary = report["summary"]
    if summary["cases"] < 1 or summary["candidate_count"] < summary["cases"]:
        return "missing prepared candidates"
    if summary["timeout_rate"] or summary["error_rate"]:
        return "generation errors or timeouts"
    for name in ("valid_output_schema", "conventional_commit", "allowed_scope", "subject_length", "unsupported_component", "unsupported_issue_identifier", "unsupported_test_outcome_claim", "unsupported_behavioral_claim", "daemon_grounding_coverage"):
        check = summary[name]
        if check["checked"] < 1 or check["passed"] != check["checked"]:
            return f"failed automatic gate: {name}"
    if summary["warm_p95_ms"] > warm_budget_ms:
        return "warm pipeline p95 exceeds selected budget"
    return ""


def run_qualification(args):
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.:/-]{0,119}", args.model):
        raise ValueError("select an explicit already-installed model tag")
    if not (1024 <= args.context_tokens <= 32768 and 64 <= args.output_tokens <= 2048 and 1 <= args.warm_timeout <= 60 and 1 <= args.cold_timeout <= 120 and 0 <= args.keep_alive <= 1800):
        raise ValueError("generation budget outside supported bounds")
    args.output_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
    config = args.output_dir / "config.toml"
    config.write_text(f'[provider]\nname = "ollama"\nmodel = {json.dumps(args.model)}\ntimeout = "{args.warm_timeout}s"\nfallback = "none"\n[ollama]\ncontext_tokens = {args.context_tokens}\noutput_tokens = {args.output_tokens}\ncold_timeout = "{args.cold_timeout}s"\nkeep_alive = "{args.keep_alive}s"\n[daemon]\nmax_generation_concurrency = 1\n')
    config.chmod(0o600)
    environment = dict(os.environ, ZSH_GIT_INLAY_CONFIG=str(config.resolve()))
    identity = None
    result = {"status": "operational_checks_pending", "human_review_required": True, "model": args.model, "corpus": args.corpus, "runs": []}
    try:
        for partition, count in (("development", 1), ("held-out", 3)):
            for index in range(count):
                directory = args.output_dir / f"{partition}-{index + 1}"
                command = [str(args.binary.resolve()), "evaluate", "--fixtures", "--corpus", args.corpus, "--partition", partition, "--provider", "ollama", "--model", args.model, "--timeout", f"{args.cold_timeout + 5}s", "--output-dir", str(directory.resolve()), "--json"]
                completed = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=3600, check=True)
                paths = json.loads(completed.stdout)
                report_path = Path(paths["reports"]["json"])
                report = json.loads(report_path.read_text())
                provider = report["provider"]
                if provider["name"] != "ollama" or provider["model"] != args.model or not re.fullmatch(r"[a-f0-9]{64}", provider["model_digest"]):
                    raise ValueError("qualification must use the selected real local model without fallback")
                current_identity = (provider["model_digest"], report["corpus_digest"], report["provider"]["prompt_version"])
                if identity is not None and current_identity != identity:
                    raise ValueError("model, corpus or prompt identity changed between qualification runs")
                identity = current_identity
                failure = gate(report, args.warm_timeout * 1000)
                result["runs"].append({"partition": partition, "report": str(report_path), "failure": failure})
                if failure:
                    result["status"] = "rejected_by_operational_gate"
                    return result
        result["status"] = "operational_checks_passed_human_review_required"
        return result
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        result["status"] = "runtime_failure"
        result["failure_kind"] = type(error).__name__
        raise
    finally:
        output = args.output_dir / "qualification.json"
        with output.open("x") as file:
            json.dump(result, file, indent=2)
            file.write("\n")
        output.chmod(0o600)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=Path(".build/zsh-git-inlay"))
    parser.add_argument("--model", required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--corpus", choices=("v2", "v3"), default="v3")
    parser.add_argument("--context-tokens", type=int, default=4096)
    parser.add_argument("--output-tokens", type=int, default=256)
    parser.add_argument("--warm-timeout", type=int, default=8)
    parser.add_argument("--cold-timeout", type=int, default=30)
    parser.add_argument("--keep-alive", type=int, default=300)
    args = parser.parse_args()
    try:
        result = run_qualification(args)
        print(json.dumps(result, indent=2))
        if result["status"].startswith("rejected"):
            raise SystemExit(1)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, f"local model qualification failed: {error}\n")


if __name__ == "__main__":
    main()
