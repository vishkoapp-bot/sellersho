-- Rollback for 202610190006_pack_box_size.

ALTER TABLE orders DROP COLUMN IF EXISTS box_size;
