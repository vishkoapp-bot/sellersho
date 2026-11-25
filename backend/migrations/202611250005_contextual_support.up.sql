ALTER TABLE support_tickets
    ADD COLUMN requester_account_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN category TEXT NOT NULL DEFAULT 'general',
    ADD COLUMN reference_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN reference_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN reference_label TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT support_tickets_context_check CHECK (
        (category = 'general' AND reference_type = '' AND reference_id = '' AND reference_label = '')
        OR (category IN ('order', 'shipment', 'return', 'financial')
            AND requester_account_id <> '' AND reference_type <> '' AND reference_id <> '' AND reference_label <> '')
    );

CREATE INDEX support_tickets_supplier_account_idx
    ON support_tickets (requester_user_id, requester_account_id, created_at DESC, id DESC)
    WHERE requester_role = 'supplier';
