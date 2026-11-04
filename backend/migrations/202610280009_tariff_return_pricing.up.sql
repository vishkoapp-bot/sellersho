-- Tapin return settlement (Oct 28, 2026): tariff-based return pricing.
--
-- The return price is computed from the NATIONAL POST COMPANY tariff for
-- 1405 ("نرخنامه خدمات پستی"), NOT from a Tapin check-price quote.
-- Every price in that PDF is a CEILING ("سقف تعرفه") — a maximum, not
-- necessarily the exact amount the customer pays at the post office.
-- The displayed/settled return price is therefore the ceiling-based
-- figure; the return_price_reviews table below lets the customer ask
-- for a review with the invoice of the amount actually paid.
--
-- Pricing service (documented in the mission report + api-contract.md):
--   Table 4, پیشتاز (D+3), بسته (پاکت جوف/بسته) row.
--   Bands: درون‌استانی / برون‌استانی همجوار / برون‌استانی غیرهمجوار.
--   Weight: base up to 1 kg, then per kg or fraction over 1 kg.
--   Surcharges (all % of the BASE service rate, additive, never
--   compounding): carton tier (Table 5 row 2), distribution (Table 6
--   note 24), dispatch (Table 6 note 25), island air/sea fee
--   (Table 4 note 8), mandatory compensation fee (Table 7 row 2).
-- Table 1 (امانت پستی, non-priority) is ingested for REFERENCE only —
-- it is not priced from. Its row 7 is reproduced EXACTLY as printed
-- (intra-province 270,000 Rial for 3–5 kg, LOWER than row 6's 320,000
-- for 2–3 kg) — this is an anomaly in the source PDF, flagged in the
-- report, deliberately NOT "fixed" here.

ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS tapin_tariff_url TEXT;

-- First/current tariff link: the 1405 national post tariff PDF.
UPDATE platform_settings
   SET tapin_tariff_url = 'https://tapin.ir/blog/wp-content/uploads/2026/03/%D9%86%D8%B1%D8%AE%D9%86%D8%A7%D9%85%D9%871405.pdf'
 WHERE id = 1 AND tapin_tariff_url IS NULL;

CREATE TABLE IF NOT EXISTS tapin_tariff_versions (
    id            SERIAL PRIMARY KEY,
    source_url    TEXT NOT NULL,
    source_label  TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('pending', 'active', 'superseded')),
    effective_from DATE NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at  TIMESTAMPTZ,
    confirmed_by  TEXT,
    note          TEXT
);

-- Exactly one active tariff version at a time.
CREATE UNIQUE INDEX IF NOT EXISTS tapin_tariff_versions_one_active
    ON tapin_tariff_versions (status) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS tapin_tariff_rates (
    id                    SERIAL PRIMARY KEY,
    version_id            INT NOT NULL REFERENCES tapin_tariff_versions (id) ON DELETE CASCADE,
    service               TEXT NOT NULL, -- 'pishtaz' (priced) | 'amanat' (reference)
    parcel_kind           TEXT NOT NULL, -- 'baste' (package) | 'packet'
    band                  TEXT NOT NULL CHECK (band IN ('intra', 'adjacent', 'nonadjacent')),
    -- Pishtaz form: base up to 1 kg + per kg (or fraction) over 1 kg.
    base_upto_1kg_rial    BIGINT,
    per_kg_over_rial      BIGINT,
    -- Amanat form: flat price for a weight band (NULL = unused form).
    min_weight_grams      INT,
    max_weight_grams      INT,
    flat_price_rial       BIGINT,
    note                  TEXT,
    UNIQUE (version_id, service, parcel_kind, band, min_weight_grams, max_weight_grams),
    CHECK (
        (base_upto_1kg_rial IS NOT NULL AND per_kg_over_rial IS NOT NULL AND flat_price_rial IS NULL)
        OR
        (flat_price_rial IS NOT NULL AND base_upto_1kg_rial IS NULL AND per_kg_over_rial IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS tapin_tariff_surcharges (
    id            SERIAL PRIMARY KEY,
    version_id    INT NOT NULL REFERENCES tapin_tariff_versions (id) ON DELETE CASCADE,
    code          TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('percent_of_base', 'flat_rial', 'per_kg_rial')),
    value         BIGINT NOT NULL,
    applies_to    TEXT NOT NULL,
    provinces     JSONB,
    cities        JSONB,
    note          TEXT,
    UNIQUE (version_id, code)
);

-- Price review: the customer disputes the ceiling-based figure with the
-- invoice of the amount actually paid. One pending review and at most
-- one accepted review per (return, group). The warehouse keeper decides
-- (they hold the parcel + invoice); the super-admin oversees via list.
CREATE TABLE IF NOT EXISTS return_price_reviews (
    id                  UUID PRIMARY KEY,
    return_id           UUID NOT NULL REFERENCES customer_returns (id) ON DELETE CASCADE,
    group_key           TEXT NOT NULL,
    customer_id         UUID NOT NULL,
    proposed_price_toman BIGINT NOT NULL CHECK (proposed_price_toman > 0),
    invoice_url         TEXT,
    note                TEXT,
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
    agreed_price_toman  BIGINT CHECK (agreed_price_toman IS NULL OR agreed_price_toman > 0),
    decided_by          TEXT,
    decision_reason     TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS return_price_reviews_one_pending
    ON return_price_reviews (return_id, group_key) WHERE status = 'pending';
CREATE UNIQUE INDEX IF NOT EXISTS return_price_reviews_one_accepted
    ON return_price_reviews (return_id, group_key) WHERE status = 'accepted';
CREATE INDEX IF NOT EXISTS return_price_reviews_return_idx
    ON return_price_reviews (return_id, status);

-- Seed version 1: the 1405 national post tariff (PDF read Oct 28, 2026).
-- All amounts in RIAL. Idempotent: skipped when ANY version with the
-- same source URL already exists (re-applying after a test superseded
-- v1 must not violate the one-active-version index).
DO $$
DECLARE
    v_id INT;
BEGIN
    SELECT id INTO v_id FROM tapin_tariff_versions
     WHERE source_url = 'https://tapin.ir/blog/wp-content/uploads/2026/03/%D9%86%D8%B1%D8%AE%D9%86%D8%A7%D9%85%D9%871405.pdf'
     ORDER BY id LIMIT 1;
    IF v_id IS NULL THEN
        INSERT INTO tapin_tariff_versions (source_url, source_label, status, effective_from, confirmed_at, note)
        VALUES (
            'https://tapin.ir/blog/wp-content/uploads/2026/03/%D9%86%D8%B1%D8%AE%D9%86%D8%A7%D9%85%D9%871405.pdf',
            'نرخنامه خدمات پستی ۱۴۰۵ — شرکت ملی پست',
            'active',
            DATE '2026-03-21',
            NOW(),
            'سقف تعرفه (ceiling). Table 4 پیشتاز بسته is the priced service; Table 1 امانت is reference only. Row 7 anomaly reproduced as printed.'
        ) RETURNING id INTO v_id;

        -- Table 4, پیشتاز (D+3), بسته (پاکت جوف/بسته): the priced service.
        INSERT INTO tapin_tariff_rates (version_id, service, parcel_kind, band, base_upto_1kg_rial, per_kg_over_rial)
        VALUES
            (v_id, 'pishtaz', 'baste', 'intra',       800000, 125000),
            (v_id, 'pishtaz', 'baste', 'adjacent',    900000, 155000),
            (v_id, 'pishtaz', 'baste', 'nonadjacent', 1000000, 175000);

        -- Table 1, امانت پستی ثبتی (بسته): REFERENCE ONLY, not priced.
        -- Rows: پاکت جوف/بسته 0.5–1 kg, 1–2 kg, 2–3 kg, 3–5 kg.
        INSERT INTO tapin_tariff_rates (version_id, service, parcel_kind, band, min_weight_grams, max_weight_grams, flat_price_rial, note)
        VALUES
            (v_id, 'amanat', 'baste', 'intra',       500, 1000, 205000, NULL),
            (v_id, 'amanat', 'baste', 'adjacent',    500, 1000, 285000, NULL),
            (v_id, 'amanat', 'baste', 'nonadjacent', 500, 1000, 320000, NULL),
            (v_id, 'amanat', 'baste', 'intra',       1000, 2000, 290000, NULL),
            (v_id, 'amanat', 'baste', 'adjacent',    1000, 2000, 370000, NULL),
            (v_id, 'amanat', 'baste', 'nonadjacent', 1000, 2000, 412000, NULL),
            (v_id, 'amanat', 'baste', 'intra',       2000, 3000, 320000, NULL),
            (v_id, 'amanat', 'baste', 'adjacent',    2000, 3000, 385000, NULL),
            (v_id, 'amanat', 'baste', 'nonadjacent', 2000, 3000, 449000, NULL),
            (v_id, 'amanat', 'baste', 'intra',       3000, 5000, 270000, 'ANOMALY: as printed in the source PDF — intra-province 270,000 is LOWER than the 2–3 kg row (320,000) for a heavier band. Reproduced, not fixed.'),
            (v_id, 'amanat', 'baste', 'adjacent',    3000, 5000, 455000, NULL),
            (v_id, 'amanat', 'baste', 'nonadjacent', 3000, 5000, 525000, NULL);

        -- Surcharges. % rows are % of the BASE service rate (additive).
        INSERT INTO tapin_tariff_surcharges (version_id, code, kind, value, applies_to, provinces, cities, note)
        VALUES
            -- Table 5 row 2: non-standard-dimension carton surcharge, by box size.
            (v_id, 'carton_4', 'percent_of_base', 25, 'carton_size_4', NULL, NULL, 'سایز ۴'),
            (v_id, 'carton_5', 'percent_of_base', 50, 'carton_size_5', NULL, NULL, 'سایز ۵'),
            (v_id, 'carton_6', 'percent_of_base', 75, 'carton_size_6', NULL, NULL, 'سایز ۶'),
            (v_id, 'carton_7', 'percent_of_base', 100, 'carton_size_7', NULL, NULL, 'سایز ۷'),
            (v_id, 'carton_8', 'percent_of_base', 125, 'carton_size_8', NULL, NULL, 'سایز ۸'),
            (v_id, 'carton_9', 'percent_of_base', 150, 'carton_size_9', NULL, NULL, 'سایز ۹'),
            (v_id, 'carton_10plus', 'percent_of_base', 200, 'carton_size_10plus', NULL, NULL, 'بزرگ‌تر از سایز ۹ یا غیراستاندارد'),
            -- Table 6 note 24: distribution surcharge by DESTINATION.
            (v_id, 'dist_tehran_alborz', 'percent_of_base', 20, 'destination_province', '["تهران", "البرز"]'::jsonb, NULL, 'مقصد در استان تهران یا البرز'),
            (v_id, 'dist_metropolis', 'percent_of_base', 15, 'destination_city', NULL, '["مشهد", "اصفهان", "شیراز", "تبریز", "قم", "اهواز", "کرمانشاه"]'::jsonb, 'مقصد در سایر کلانشهرها (جمعیت بالای ۱ میلیون؛ فهرست قابل تأیید مدیر)'),
            -- Table 6 note 25: dispatch surcharge by ORIGIN province.
            (v_id, 'dispatch_vast', 'percent_of_base', 5, 'origin_province', '["کرمان", "سیستان و بلوچستان", "فارس", "خراسان جنوبی", "خراسان رضوی", "اصفهان", "سمنان", "یزد"]'::jsonb, NULL, 'مبدأ در استان‌های پهناور فهرست‌شده'),
            -- Table 4 note 8 / Table 1 row 12: island air/sea fee, per kg (or fraction).
            (v_id, 'island_air_sea', 'per_kg_rial', 255000, 'island_origin_or_destination', NULL, '["کیش", "قشم", "هرمز", "خارگ", "ابوموسی", "تنب بزرگ", "تنب کوچک", "لاوان", "سیری", "هنگام"]'::jsonb, 'مبدأ یا مقصد جزیره باشد؛ به ازای هر کیلوگرم و کسر آن'),
            -- Table 7 row 2: mandatory compensation fee, registered بسته without declared value (coverage up to 100M Rial).
            (v_id, 'compensation_baste', 'flat_rial', 100000, 'registered_baste', NULL, NULL, 'تعهد غرامت اجباری بسته ثبتی بدون اظهار ارزش');
    END IF;
END $$;
