-- ============================================================================
-- 202610180005 — notification_outbox (FIXWAVE P2, Fix 12)
-- ============================================================================
-- ⚠️  NOT APPLIED TO PROD — deploy with the P2 fix wave. The backend must
--      not be restarted against a DB missing this table: every NotifyUser
--      call runs inside an existing money transaction, so the INSERT would
--      fail and roll the transaction back (fail-closed).
--
-- Minimal transactional notification outbox:
--   - writer: repository.NotifyUser(tx/db, recipientUserID, kind, title,
--     body, payloadJSON) — called inside existing business transactions
--     (delivery confirm, return decision, payout review, worker auto-confirm).
--   - delivery: the background worker flips 'pending' → 'sent' (visible via
--     GET /api/v1/notifications); nothing external is faked — no push/SMS.
--   - read: POST /api/v1/notifications/read flips 'sent' → 'read'.
-- ============================================================================

CREATE TABLE IF NOT EXISTS notification_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(50) NOT NULL,
    title VARCHAR(200) NOT NULL,
    body TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sent', 'read')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS notification_outbox_recipient_idx
    ON notification_outbox (recipient_user_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS notification_outbox_pending_idx
    ON notification_outbox (status, created_at)
    WHERE status = 'pending';
