CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;
-- A document can appear on multiple lines, all belonging to the same shipment.
ALTER TABLE inbound_inventory_lines ADD CONSTRAINT inbound_external_reference_one_shipment
 EXCLUDE USING gist (external_reference public.gist_text_ops WITH =, shipment_id public.gist_text_ops WITH <>)
 WHERE (external_reference <> '');
DROP INDEX inbound_external_reference_unique;
