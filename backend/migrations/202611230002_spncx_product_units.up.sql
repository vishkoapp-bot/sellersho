-- 202611230002_spncx_product_units.up.sql
CREATE TABLE product_units (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            VARCHAR(20) NOT NULL UNIQUE,
    variant_id      UUID NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
    product_id      VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    sequence        INT NOT NULL CHECK (sequence BETWEEN 1 AND 9999),
    status          TEXT NOT NULL DEFAULT 'defined'
                    CHECK (status IN ('defined','in_stock','reserved','sold',
                                     'returned','quarantined','damaged','removed')),
    balance_id      VARCHAR(80) REFERENCES inventory_balances(id) ON DELETE RESTRICT,
    printed_at      TIMESTAMPTZ,
    last_printed_at TIMESTAMPTZ,
    print_count     INT NOT NULL DEFAULT 0 CHECK (print_count >= 0),
    printed_by      VARCHAR(50),
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (code ~ '^[0-9A-Z-]+-X[0-9]{4}$'),
    -- A removed unit holds no lot; every other unit holds exactly one lot.
    CHECK ((status = 'removed') = (balance_id IS NULL)),
    UNIQUE (variant_id, sequence)
);

CREATE INDEX product_units_variant_status_idx
    ON product_units (variant_id, status);
CREATE INDEX product_units_balance_idx
    ON product_units (balance_id) WHERE balance_id IS NOT NULL;
CREATE INDEX product_units_product_idx
    ON product_units (product_id, status);
-- Unprinted sellable units, in label-print order (sequence ascending).
CREATE INDEX product_units_unprinted_idx
    ON product_units (variant_id, sequence)
    WHERE printed_at IS NULL AND status = 'in_stock';

-- Per-variant sequence allocator (§2.2). One row per variant, created lazily
-- on first mint. next_sequence = 10000 means the variant is exhausted.
CREATE TABLE product_variant_unit_counters (
    variant_id    UUID PRIMARY KEY REFERENCES product_variants(id) ON DELETE RESTRICT,
    next_sequence INT NOT NULL DEFAULT 1 CHECK (next_sequence BETWEEN 1 AND 10000),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only unit audit. inventory_movements stays the lot-level audit;
-- this is the unit-level history the lookup endpoint returns (§7).
CREATE TABLE product_unit_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    unit_id         UUID NOT NULL REFERENCES product_units(id) ON DELETE RESTRICT,
    code            VARCHAR(20) NOT NULL,
    event_type      TEXT NOT NULL,
    from_status     TEXT,
    to_status       TEXT,
    reference_type  TEXT NOT NULL,
    reference_id    TEXT,
    actor_user_id   VARCHAR(50),
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX product_unit_events_unit_idx ON product_unit_events (unit_id, created_at);
CREATE INDEX product_unit_events_code_idx ON product_unit_events (code, created_at);

-- Which physical units a paid order consumed. Populated at capture (§3.4);
-- restored_at is set by cancel / return-restock for the same units.
CREATE TABLE order_item_units (
    order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE RESTRICT,
    unit_id       UUID NOT NULL REFERENCES product_units(id) ON DELETE RESTRICT,
    code          VARCHAR(20) NOT NULL,
    order_id      VARCHAR(50) NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    restored_at   TIMESTAMPTZ,
    PRIMARY KEY (order_item_id, unit_id)
);
CREATE INDEX order_item_units_unit_idx ON order_item_units (unit_id);
CREATE INDEX order_item_units_order_idx ON order_item_units (order_id);

-- Backfill run state (resumability) + pre-migration snapshot (rollback). §5.
CREATE TABLE spncx_backfill_state (
    product_id   VARCHAR(50) PRIMARY KEY REFERENCES products(id) ON DELETE RESTRICT,
    status       TEXT NOT NULL CHECK (status IN ('pending','completed','failed','excluded')),
    -- 'excluded' (P1-6): durable owner veto. Set by the admin console (§4.4 step 5) with
    -- excluded_reason + excluded_by + excluded_at (columns below, added in this migration);
    -- a resume-all run never backfills an excluded product (§5.7), and V1/V5 are scoped to
    -- the non-excluded set (§5.8). The original three-state enum made the veto non-durable
    -- and the strict flip unreachable with any veto in place.
    excluded_reason TEXT,
    excluded_by   VARCHAR(50),
    excluded_at   TIMESTAMPTZ,
    -- Per-product excess decision (P1-b, fix round 2): the durable record of the §5.6
    -- blocking decision. excess_decision NULL = undecided. Set pre-run via
    -- POST /api/v1/admin/spncx/backfill/excess-decision (§5.6); the run refuses a
    -- product with excess_amount > 0 and excess_decision NULL. V6b (§5.8) fails while
    -- any excess is undecided past review_by (completed_at + 30 days).
    excess_amount     INT NOT NULL DEFAULT 0,
    excess_decision   TEXT CHECK (excess_decision IN ('write_off_accepted','physical_count_ordered')),
    excess_decided_by VARCHAR(50),
    excess_decided_at TIMESTAMPTZ,
    units_minted INT NOT NULL DEFAULT 0,
    units_target INT NOT NULL,
    plan         JSONB NOT NULL DEFAULT '{}',
    -- Plan hash (S2-M1, fix round 3): the dry-run plan hash (§5.6/P1-3 — covers the
    -- product set, the OQ-1 basis, and the exclusion/veto set only). The `plan` column
    -- holds exactly the per-product `products[]` element (§10.1) and never the hash;
    -- the hash lives in this separate column, the same value on every row of one dry run.
    plan_hash    TEXT,
    error        TEXT,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);
CREATE TABLE spncx_backfill_snapshot (
    product_id VARCHAR(50) PRIMARY KEY REFERENCES products(id) ON DELETE RESTRICT,
    snapshot   JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Durable print-job record (P0-5). Print idempotency MUST NOT live only in event
-- metadata: there was no unique constraint on the key, no index on
-- metadata->>'idempotency_key', and no atomic check-and-insert, so two concurrent POSTs
-- with the same key could both look up before either wrote events, both mark, and both
-- increment print_count (and a metadata-JSONB lookup is a full scan of an unbounded
-- append-only table on every print retry). This table is the durable record.
CREATE TABLE spncx_print_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL UNIQUE,
    product_id      VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    variant_id      UUID REFERENCES product_variants(id) ON DELETE RESTRICT,
    receipt_line_id TEXT,
    unit_codes      JSONB NOT NULL DEFAULT '[]',
    payload         JSONB NOT NULL DEFAULT '{}',
    actor_user_id   VARCHAR(50),
    reprint         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX spncx_print_jobs_product_idx ON spncx_print_jobs (product_id, created_at);
-- One row per (job, unit), for the atomic per-unit claim (§6.3). A second job claiming
-- the same unit for a non-reprint print is rejected (409 already_printed_in_job).
CREATE TABLE spncx_print_job_units (
    job_id  UUID NOT NULL REFERENCES spncx_print_jobs(id) ON DELETE RESTRICT,
    unit_id UUID NOT NULL REFERENCES product_units(id) ON DELETE RESTRICT,
    code    VARCHAR(20) NOT NULL,
    PRIMARY KEY (job_id, unit_id)
);

ALTER TABLE inventory_balances ADD COLUMN unit_tracked BOOLEAN NOT NULL DEFAULT FALSE;

-- Durable manual-resolution queue for payment confirmation failures.
CREATE TABLE spncx_failed_captures (
    order_id VARCHAR(50) PRIMARY KEY REFERENCES orders(id) ON DELETE RESTRICT,
    failure TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('strict','pre_strict')),
    at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The ASN migration replaced the earlier CHECK that allowed sold lots.
ALTER TABLE inventory_balances DROP CONSTRAINT inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check
    CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine',
                      'in_transit','damaged','unreconciled','inbound_reserved','qc_accepted','sold'));
