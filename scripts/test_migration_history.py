import unittest

from scripts.migration_history import classify_history


class MigrationHistoryTests(unittest.TestCase):
    def test_accepts_only_pinned_alias_with_present_canonical_target(self):
        canonical = {"001_initial.up.sql", "002_next.up.sql"}
        aliases = {"001": "001_initial.up.sql"}

        pending, accepted_aliases = classify_history(
            {"001", "001_initial.up.sql"}, canonical, aliases
        )

        self.assertEqual(pending, ["002_next.up.sql"])
        self.assertEqual(accepted_aliases, {"001"})

    def test_rejects_alias_when_canonical_target_is_missing_from_history(self):
        with self.assertRaisesRegex(ValueError, "canonical target is not applied"):
            classify_history(
                {"001"}, {"001_initial.up.sql"}, {"001": "001_initial.up.sql"}
            )

    def test_rejects_unknown_history(self):
        with self.assertRaisesRegex(ValueError, "unreviewed migration history"):
            classify_history(
                {"unexpected"}, {"001_initial.up.sql"}, {}
            )


if __name__ == "__main__":
    unittest.main()
