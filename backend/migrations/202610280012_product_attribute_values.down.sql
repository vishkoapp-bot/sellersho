-- 202610280012 DOWN — reverses the product attribute-values link table.
--
-- Safe to drop outright: nothing else references
-- product_attribute_values (its only FK points INTO products), so
-- dropping it cannot orphan product or proposal rows. The suppliers'
-- original picks remain in pending_approvals.requested_data
-- (categoryAttributes) and would be re-materialised by a re-approval
-- after UP is re-applied.

DROP TABLE IF EXISTS product_attribute_values;
