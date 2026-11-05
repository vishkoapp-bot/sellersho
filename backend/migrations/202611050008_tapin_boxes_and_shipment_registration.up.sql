-- 202611050008: Tapin completion (gaps 1, 2, 3).
--
-- tapin_boxes: the PLATFORM-managed postal-box catalogue. Tapin's API
-- has no boxes endpoint (verified against Tapin's official Tipax
-- implementation guide, 2026-11-05 — see
-- ~/workspace/tapin-spec/tapin-contract-capture.md), so the catalogue
-- lives here and is edited by admins. Seeded with ONLY the one box
-- whose dimensions the owner has confirmed: postal box «سایز ۴»
-- (30x20x20 cm), which is also the platform default
-- (platform_settings.tapin_default_box_id = '4'). No other sizes are
-- invented; admins add real boxes from the panel.
--
-- supplier_shipments tapin_* columns: where a Tapin-registered
-- shipment records how it was registered (service, chosen box,
-- registration time, last upstream order status). The barcode itself
-- stays in tracking_number and the carrier in carrier
-- ('tapin-post' / 'tapin-tipax'), exactly like a manual shipment.
-- The unknown-outcome guard column tapin_register_attempted_at
-- already exists (migration 202610250012).

CREATE TABLE IF NOT EXISTS tapin_boxes (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    length_cm       INT NOT NULL,
    width_cm        INT NOT NULL,
    height_cm       INT NOT NULL,
    max_weight_grams INT,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    sort            INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO tapin_boxes (id, title, length_cm, width_cm, height_cm, max_weight_grams, is_active, sort)
VALUES ('4', 'سایز ۴', 30, 20, 20, NULL, TRUE, 0)
ON CONFLICT (id) DO NOTHING;

ALTER TABLE supplier_shipments
    ADD COLUMN IF NOT EXISTS tapin_service TEXT,
    ADD COLUMN IF NOT EXISTS tapin_box_id TEXT,
    ADD COLUMN IF NOT EXISTS tapin_registered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS tapin_last_status TEXT;
