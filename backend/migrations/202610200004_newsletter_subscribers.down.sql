-- Down for 202610200004 (newsletter_subscribers).

DROP INDEX IF EXISTS idx_newsletter_subs_token;
DROP INDEX IF EXISTS idx_newsletter_subs_store_status;
DROP TABLE IF EXISTS newsletter_subscribers;
