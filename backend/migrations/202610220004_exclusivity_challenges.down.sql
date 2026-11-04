-- Down for 202610220004 (exclusivity challenges): drop the dedicated
-- challenge table. Any pending/approved challenges and their waiver
-- consumption state are discarded by the rollback — that is the point of
-- rolling back the feature; no other table references this table.

DROP TABLE IF EXISTS exclusivity_challenges;
