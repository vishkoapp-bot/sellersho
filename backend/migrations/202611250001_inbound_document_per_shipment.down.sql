-- Refuse to downgrade if doing so would invalidate existing multi-line documents.
CREATE UNIQUE INDEX inbound_external_reference_unique ON inbound_inventory_lines(external_reference) WHERE external_reference <> '';
ALTER TABLE inbound_inventory_lines DROP CONSTRAINT inbound_external_reference_one_shipment;
