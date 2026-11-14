# Supplier warehouse geography

Migration `202611140007` adds nullable integer `tapin_province_id` and
`tapin_city_id` fields to supplier warehouses. Existing Tapin repository readers
already request these columns; a fresh migrated database previously failed with
PostgreSQL undefined-column error 42703. Central warehouses already use the same
integer types.

The migration does not infer geography from names, backfill provider identifiers
or add defaults. Unknown values remain NULL and existing repository responses
retain their zero/unmapped sentinel. Explicit recorded identifiers round-trip
through both supplier warehouse lookup paths. Rollback refuses any recorded
mapping rather than discarding it.

The guarded regression uses the actual canonical migrated schema, proves
unknown compatibility and explicit mappings, checks column types/nullability,
and attempts rollback within an always-rolled-back transaction. It first failed
with error 42703 and then passed after the additive migration. This is a local
schema correctness checkpoint, not carrier certification or a capacity result.
No production mapping or service was changed.
