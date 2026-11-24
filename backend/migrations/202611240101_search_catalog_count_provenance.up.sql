-- Old binaries copied the store scope's count into both columns. Preserve
-- those historical values but never infer central absence from them.
ALTER TABLE search_queries
  ADD COLUMN IF NOT EXISTS catalog_result_count_verified BOOLEAN NOT NULL DEFAULT FALSE;
