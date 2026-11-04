-- 202610200004: per-store newsletter subscribers (store-gaps spec §۱.۲).
-- One row per (store, mobile): subscribing in store A never touches store B.
-- The unsubscribe token is the only credential the public unsubscribe
-- endpoint needs.

CREATE TABLE IF NOT EXISTS newsletter_subscribers (
    id              VARCHAR(50) PRIMARY KEY,               -- 'sub-<uuid>', app-generated
    store_id        VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    mobile          VARCHAR(15) NOT NULL,                  -- normalized ASCII 09xxxxxxxxx
    email           VARCHAR(254),                          -- optional, lowercased
    status          VARCHAR(12) NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','unsubscribed')),
    source          VARCHAR(20) NOT NULL DEFAULT 'footer', -- footer | section
    unsubscribe_token VARCHAR(64) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unsubscribed_at TIMESTAMPTZ,
    UNIQUE (store_id, mobile)
);
CREATE INDEX IF NOT EXISTS idx_newsletter_subs_store_status
    ON newsletter_subscribers (store_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_newsletter_subs_token
    ON newsletter_subscribers (unsubscribe_token);
