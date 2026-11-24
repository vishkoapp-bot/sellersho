import unittest

from candidate_gate import REPO, successful_run, verify_context


SHA = "a" * 40
BASE = {
    "id": 42, "run_attempt": 2, "head_sha": SHA, "head_branch": "main",
    "event": "push", "status": "completed", "conclusion": "success",
    "head_repository": {"full_name": REPO},
}


class CandidateGateTest(unittest.TestCase):
    def test_canonical_manual_dispatch_for_checked_out_full_sha(self):
        verify_context("workflow_dispatch", REPO, "refs/heads/main", SHA, SHA)

    def test_reject_incorrect_or_untrusted_dispatch(self):
        for args in (
            ("push", REPO, "refs/heads/main", SHA, SHA),
            ("workflow_dispatch", "fork/sellersho", "refs/heads/main", SHA, SHA),
            ("workflow_dispatch", REPO, "refs/tags/main", SHA, SHA),
            ("workflow_dispatch", REPO, "refs/heads/feature", SHA, SHA),
            ("workflow_dispatch", REPO, "refs/heads/main", SHA[:8], SHA),
            ("workflow_dispatch", REPO, "refs/heads/main", "b" * 40, SHA),
        ):
            with self.subTest(args=args), self.assertRaises(ValueError):
                verify_context(*args)

    def test_reports_exact_successful_push_attempt(self):
        self.assertEqual(successful_run({"workflow_runs": [BASE]}, SHA),
                         f"https://github.com/{REPO}/actions/runs/42/attempts/2")

    def test_refuses_missing_ambiguous_failed_or_unrelated_evidence(self):
        bad_runs = [[], [BASE, BASE]]
        for key, value in (
            ("head_sha", "b" * 40),
            ("head_branch", "feature"),
            ("event", "pull_request"),
            ("head_repository", {"full_name": "fork/sellersho"}),
            ("status", "queued"),
            ("conclusion", "failure"),
            ("conclusion", "cancelled"),
            ("conclusion", "skipped"),
            ("run_attempt", 0),
        ):
            bad_runs.append([{**BASE, key: value}])
        for runs in bad_runs:
            with self.subTest(runs=runs), self.assertRaises(ValueError):
                successful_run({"workflow_runs": runs}, SHA)


if __name__ == "__main__":
    unittest.main()
