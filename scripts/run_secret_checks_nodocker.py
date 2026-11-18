"""SEC-01 (supervisor F-03 / critic F-07, gate G-15 clause 2): run the
non-Docker subset of scripts/test_production_secrets.py as a deploy
gate. The two Docker-dependent checks
(test_redis_runtime_keeps_unprivileged_entrypoint and
test_merged_compose_requires_and_uses_injection) are Docker-host-only:
they are NOT run here and NOT claimed as passed — they must be run on
a Docker host before a deploy that changes compose/redis behaviour
(see docs/operations/credential-lifecycle.md).

Usage: python3 scripts/run_secret_checks_nodocker.py [repo-root]
Exit 0 = all 4 runnable checks pass. Any failure/error exits non-zero
so the deploy aborts BEFORE any build, migration, or compose change.
"""
import os
import sys
import unittest

RUNNABLE = [
    "test_inventory_never_contains_source_values",
    "test_prod_uses_required_runtime_injection",
    "test_current_production_inventory_has_no_literal_credentials",
    "test_redis_healthcheck_does_not_contain_password_argument",
]
DOCKER_ONLY = [
    "test_redis_runtime_keeps_unprivileged_entrypoint",
    "test_merged_compose_requires_and_uses_injection",
]


def main() -> int:
    root = sys.argv[1] if len(sys.argv) > 1 else "."
    root = os.path.abspath(root)
    os.chdir(root)
    sys.path.insert(0, os.path.join(root, "scripts"))
    import test_production_secrets

    suite = unittest.TestSuite()
    loader = unittest.TestLoader()
    for name in RUNNABLE:
        suite.addTests(loader.loadTestsFromName(
            "test_production_secrets.ProductionSecrets." + name))
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    print("SEC-01 secret checks (non-Docker): %d run, %d failures, %d errors"
          % (result.testsRun, len(result.failures), len(result.errors)))
    print("NOT run here (Docker-host-only): " + ", ".join(DOCKER_ONLY))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
