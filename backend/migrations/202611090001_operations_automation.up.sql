-- Additive operational automation. Existing certificates and paused rewards are untouched.
CREATE TABLE store_domains (
 domain TEXT PRIMARY KEY CHECK (domain = lower(domain)),
 store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
 verification_token TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending_dns' CHECK(status IN ('pending_dns','active','disabled')),
 verified_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX store_domains_store_idx ON store_domains(store_id);

ALTER TABLE warehouse_bins
 ADD COLUMN position_x DOUBLE PRECISION CHECK(position_x BETWEEN -100000 AND 100000),
 ADD COLUMN position_y DOUBLE PRECISION CHECK(position_y BETWEEN -100000 AND 100000),
 ADD COLUMN position_z DOUBLE PRECISION CHECK(position_z BETWEEN -100000 AND 100000);
ALTER TABLE qc_discrepancies
 ADD COLUMN response_due_at TIMESTAMPTZ,
 ADD COLUMN responded_at TIMESTAMPTZ,
 ADD COLUMN escalated_at TIMESTAMPTZ;
ALTER TABLE qc_discrepancies ADD COLUMN admin_resolution TEXT NOT NULL DEFAULT '', ADD COLUMN reviewed_by TEXT;
UPDATE qc_discrepancies SET response_due_at=created_at+INTERVAL '48 hours';
ALTER TABLE qc_discrepancies ALTER COLUMN response_due_at SET DEFAULT (NOW()+INTERVAL '48 hours');
ALTER TABLE qc_discrepancies ALTER COLUMN response_due_at SET NOT NULL;
CREATE INDEX qc_response_deadline_idx ON qc_discrepancies(response_due_at) WHERE status='open' AND escalated_at IS NULL;

CREATE TABLE supplier_sla_metrics (
 supplier_id VARCHAR(50) PRIMARY KEY REFERENCES suppliers(id) ON DELETE CASCADE,
 samples INT NOT NULL, on_time INT NOT NULL,
 score NUMERIC(7,4) NOT NULL,
 eligible BOOLEAN NOT NULL, assessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE ad_fraud_keys (
 campaign_id TEXT NOT NULL, event_type TEXT NOT NULL,
 identity_key TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(campaign_id,event_type,identity_key)
);
CREATE INDEX ad_fraud_expiry_idx ON ad_fraud_keys(expires_at);
CREATE TABLE ad_request_limits (
 identity_key TEXT NOT NULL, bucket TIMESTAMPTZ NOT NULL, count INT NOT NULL,
 PRIMARY KEY(identity_key,bucket)
);
ALTER TABLE orders ADD COLUMN payment_expires_at TIMESTAMPTZ;
-- Legacy pending orders get a full window after rollout, not instant expiry.
UPDATE orders SET payment_expires_at=GREATEST(created_at+INTERVAL '15 minutes',NOW()+INTERVAL '15 minutes')
 WHERE payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured;
ALTER TABLE orders ALTER COLUMN payment_expires_at SET DEFAULT (NOW()+INTERVAL '15 minutes');
CREATE INDEX orders_payment_expiry_idx ON orders(payment_expires_at)
 WHERE payment_status='pending' AND status IN ('pending','processing');
CREATE TABLE operations_job_runs (
 id BIGSERIAL PRIMARY KEY, job TEXT NOT NULL, affected INT NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '', started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ
);
