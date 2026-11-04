-- Down migration for 202610160003_remediate_dev_seed_credentials.
--
-- This down migration is intentionally a NO-OP. The up migration destroyed
-- secrets (compromised password hashes were replaced with an unusable marker)
-- and discarded fabricated figures; neither can be faithfully restored, and
-- re-introducing publicly-known password hashes would re-open the security
-- hole this migration closed. Rolling back must never resurrect credentials.
SELECT 1;
