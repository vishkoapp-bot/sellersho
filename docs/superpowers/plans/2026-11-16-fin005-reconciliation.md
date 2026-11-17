# FIN005 reconciliation and audit

Default operation is read-only. Run against the deployed revision only after the
root-owned release checks. No bulk backfill or production repair is authorized by
this runbook. Initial production evidence reported zero disputed/stuck orders;
a legacy resolved verdict by itself does not require a write.

```sql
BEGIN TRANSACTION READ ONLY;
SELECT d.id AS dispute_id,d.order_id,d.status AS verdict_status,
 d.resolution_action,d.resolution_date,o.status AS order_status,
 o.disputed_at,o.confirmed_at,o.return_window_ends_at,
 o.profit_released,o.profit_released_at,o.referral_commission_released,
 e.evidence_kind,e.actor_user_id,e.review_reference,e.hold_at,
 (SELECT count(*) FROM disputes x WHERE x.order_id=o.id AND x.status='pending') pending_disputes,
 (SELECT count(*) FROM customer_returns r WHERE r.order_id=o.id AND r.status NOT IN ('rejected','cancelled','refunded')) active_returns,
 (SELECT count(*) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser') release_ledger_rows,
 (SELECT jsonb_agg(jsonb_build_object('result',t.result,'reason',t.reason,'at',t.created_at)) FROM dispute_order_transitions t WHERE t.order_id=o.id) reconciliation
FROM disputes d JOIN orders o ON o.id=d.order_id
LEFT JOIN dispute_order_evidence e ON e.dispute_id=d.id
WHERE (d.status='resolved' AND d.resolution_action='release_to_seller')
 OR o.status='delivery_disputed'
ORDER BY d.created_at,d.id;
SELECT o.order_id,o.purpose,o.business_id,o.destination,
 COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=o.id ORDER BY event_no DESC LIMIT 1),o.status) current_state,
 SUM(o.amount_toman) principal_toman
FROM order_refund_obligations o GROUP BY o.order_id,o.purpose,o.business_id,o.destination,o.id ORDER BY o.order_id,o.business_id;
COMMIT;
```

A reviewer must retain source IDs proving pre-hold confirmed status, actual
confirmation time, original recorded window end, and exact disputed hold linkage.
A confirmed timestamp alone, present settings, elapsed duration, or a supplied
prior status is insufficient. Reconcile release flags against the existing pair
of seller-credit/platform-debit rows before any repair; never clear flags,
rewrite principal, delete reversals, or post manual balancing credits.

After root/domain review explicitly accepts the source records, an operational
review record may be written to `audit_logs` with category `dispute`, action
`FIN005 reviewed legacy evidence`, operator equal to the authenticated reviewer,
and JSON details containing `disputeId`, `orderId`, `priorStatus` (must be
`delivered_confirmed`), `confirmedAt`, `windowEndsAt`, `holdAt` (RFC3339 timestamps)
and `sourceRecordIds` (retained authoritative audit record IDs). The review row
must be distinct from its sources. This is an explicit reviewed import, never a
migration inference. Keep evidence references free of credentials/customer secrets.

Invoke the internal `ReconcileReviewedDispute(ctx, disputeID, reviewRecordID,
actorUserID)` only for that reviewed record. There is no public endpoint. It
locks the complete financial prefix, checks the current hold, copies reviewed
facts/reference into immutable evidence and calls the same restoration/release
guards used by the resolver/worker. Missing provenance appends visible
`needs_review`; active claims, invalid return proof, missing windows, and
unproven non-return refunds never authorize release. Repeat calls must not
create another credit. Rerun the read-only audit and verify the stored original
confirmation/window, exactly one release pair when positive profit is released,
FIN001 signed balance, and preserved FIN003 obligations/outcomes.

During mixed-version deployment, migration160002 acquires parent-first
ACCESS EXCLUSIVE locks with NOWAIT. Contention aborts the whole migration;
retry the complete migration runner after the conflicting transaction exits.
Never retry inside an aborted transaction. Old protected mutations fail with
`FIN005-writer-required`, including after a binary rollback. Leave additive
fences installed and roll forward. Populated evidence cannot be down-migrated.
Unrelated service reads and note/tracking updates remain available.

The worker retains100 candidates and300ms per candidate. Held active claims
are excluded from due scanning. Durable review cases require reviewed evidence,
not a worker hot loop. PSP sandbox/provider execution is outside FIN005 tests;
a reserved refund is a liability/request, not proof of a bank payment.
