DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dispute_order_evidence) OR EXISTS(SELECT 1 FROM dispute_financial_decisions) OR EXISTS(SELECT 1 FROM dispute_order_transitions) THEN RAISE EXCEPTION 'FIN005 populated evidence cannot be removed'; END IF;
END $$;
DROP TRIGGER fin005_disputes_fence ON disputes;
DROP TRIGGER fin005_returns_fence ON customer_returns;
DROP TRIGGER fin005_orders_fence ON orders;
DROP TRIGGER fin005_dispute_consistency ON disputes;
DROP TRIGGER fin005_reversal_immutable ON customer_return_financial_events;
DROP TRIGGER fin005_linked_ledger_immutable ON ledger_entries;
DROP FUNCTION fin005_writer_fence(),fin005_dispute_consistency(),fin005_linked_ledger_immutable();
DROP FUNCTION fin005_return_entitlement_proven(TEXT,TEXT);
DROP TABLE dispute_order_transitions,dispute_financial_decisions,dispute_order_evidence;
DROP FUNCTION fin005_immutable(),fin005_decision_consistency();
DROP INDEX fin005_pending_disputes;
DROP INDEX fin005_active_returns;

DROP INDEX fin005_due_release;
