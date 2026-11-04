-- Persist supplier product barcodes on the products table (Fix 8, data-loss audit 20261018).
-- The barcode was collected and validated in the catalog dossier but never
-- persisted; BarcodeExists also queried this nonexistent column and the error
-- was swallowed, making the uniqueness check decorative. The column is
-- written by ApproveProposal (proposal->product promotion) and read by
-- BarcodeExists and the product catalog scans.
ALTER TABLE products ADD COLUMN IF NOT EXISTS barcode VARCHAR(64);

-- Partial unique index: barcodes must be unique when present, but legacy
-- rows and barcode-less products are unaffected. This is the final guard
-- behind the pre-approval BarcodeExists check.
CREATE UNIQUE INDEX IF NOT EXISTS idx_products_barcode_unique
ON products(barcode) WHERE barcode IS NOT NULL AND TRIM(barcode) <> '';

COMMENT ON COLUMN products.barcode IS 'بارکد محصول (EAN/UPC) — collected at proposal, verified in dossier, persisted at approval';
