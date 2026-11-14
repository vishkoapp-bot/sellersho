# Exact inbound putaway checkpoint (INV-09)

Every new positive QC acceptance delta creates one immutable putaway allocation,
one task and one isolated `qc_accepted` balance at `qc-accepted:<disposition ID>`.
The allocation records receipt line, disposition, shipment, supplier, warehouse,
product/SKU and exact quantity. Later receipts and cumulative QC events create
separate tasks; they cannot resize or supply a short earlier task. Failed units
remain damaged and undisposed units quarantined. Neither is sellable.

Actual AssignPutawayTask and ConfirmPutaway use supplier/product/SKU/header/task
guards before physical rows. Confirmation locks balances, bin, capacity
reservation and positions in shared table/ID order. It requires an assigned task,
matching immutable warehouse ID, unchanged dimensions/reservation, sufficient
current bin capacity and the complete exact acceptance source. It consumes that
source, credits bin sellable inventory, records one business movement, credits
storage coverage and consumes the reservation in the same transaction. A completed
task replays without another movement, occupancy increment or position credit.
Suggestions use the allocation's physical warehouse ID, including carton tasks.

Migration 202611140009 is additive. It does not demote historical staging, move
paid captures, backfill tasks, repair shortages or delete inventory. Allocation
updates/deletes, allocated task identity changes, reopening and completion without
the exact movement are rejected. Down migration refuses any used allocation.
`inbound_putaway_reconciliation` exposes old pending tasks and accepted history
without exact task evidence. The warehouse queue displays these as blocked
reconciliation entries; ConfirmPutaway refuses them. Their quantities represent
recorded evidence and are not a promise of current available stock.

Carton identity remains on the immutable allocation. The older storage-position
`inbound_id` references ASN headers only, so carton placement leaves that field
NULL rather than inserting a carton UUID into the ASN foreign key. Exact SKU
storage cohorts/receipt-age charging and placement departure occupancy still
belong to INV-10/FIN-006/INV-06. This checkpoint does not close those tasks or
INV-08's effective verified SKU measurement requirement.

The seven retained downstream branches now run against canonical migrated
databases through actual receipt, QC, assignment and confirmation. The SKU return
probe also verifies exact restored capture and replay. Legacy birth-origin modes
include a cleared mutable header and an already-debited supplier source: QC
consumes the immutable received allocation and never debits the supplier twice.
The obsolete compatibility marker and unused physical-to-sellable helper have
been removed. The fail-only QC endpoint still refuses requests; cumulative
CompleteQC handles failures through the conserved disposition protocol. INV-09
remains open pending the complete combined inventory/source acceptance evidence.

Evidence: `logs/inv09-before.log` demonstrates both legacy QC endpoints produced
no tasks before the fix. `logs/inv09-checkpoint-final.log` ran 23 focused top-level
cases with zero failures, including real legacy and tracked receipt/QC/putaway/
payment paths, partial pass/fail, deficient-source rollback, reduced-capacity
refusal, concurrent replay, immutable evidence, migration history preservation,
storage coverage/top-up and actual storage fee assessment. Frontend build passed;
`logs/inv09-frontend-test.log` reports 285 tests passed, zero failed/skipped.
Default Go suite passed in `logs/inv09-default-go.log` with DB integration opt-out;
that run alone is not database acceptance evidence.
