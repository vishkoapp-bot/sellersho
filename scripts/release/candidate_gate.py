#!/usr/bin/env python3
"""Fail closed unless GitHub's newest CI push for this exact main SHA passed."""
import json
import os
import re
import subprocess
import sys


REPO = "vishkoapp-bot/sellersho"


def verify_context(event, repository, ref, expected, checked_out):
    if event != "workflow_dispatch" or repository != REPO or ref != "refs/heads/main":
        raise ValueError("candidate must be manually dispatched on canonical main")
    if not re.fullmatch(r"[0-9a-f]{40}", expected) or expected != checked_out:
        raise ValueError("input must match the full dispatched main commit SHA")


def successful_run(payload, expected):
    runs = payload.get("workflow_runs")
    if not isinstance(runs, list) or len(runs) != 1:
        raise ValueError("no unambiguous latest CI push run for this commit")
    run = runs[0]
    if (
        run.get("head_sha") != expected
        or run.get("head_branch") != "main"
        or run.get("event") != "push"
        or run.get("status") != "completed"
        or run.get("conclusion") != "success"
        or run.get("head_repository", {}).get("full_name") != REPO
        or type(run.get("id")) is not int
        or run["id"] <= 0
        or type(run.get("run_attempt")) is not int
        or run["run_attempt"] <= 0
    ):
        raise ValueError("latest exact-SHA CI push is not a successful canonical run")
    return f"https://github.com/{REPO}/actions/runs/{run['id']}/attempts/{run['run_attempt']}"


def main():
    expected = os.environ.get("EXPECTED_COMMIT", "")
    checked_out = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    verify_context(os.environ.get("GITHUB_EVENT_NAME"), os.environ.get("GITHUB_REPOSITORY"),
                   os.environ.get("GITHUB_REF"), expected, checked_out)
    if checked_out != os.environ.get("GITHUB_SHA"):
        raise ValueError("checked out commit must match the dispatch revision")
    response = subprocess.check_output([
        "gh", "api", "--method", "GET", f"repos/{REPO}/actions/workflows/ci.yml/runs",
        "-f", "branch=main", "-f", "event=push", "-f", f"head_sha={expected}", "-F", "per_page=1",
    ], text=True)
    print(successful_run(json.loads(response), expected))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, TypeError, AttributeError, OSError, subprocess.CalledProcessError) as exc:
        print(f"Release candidate refused: {exc}", file=sys.stderr)
        sys.exit(1)
