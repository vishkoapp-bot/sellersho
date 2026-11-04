-- 202610200007: homepage identity wave-B (user-approved, 2026-10-20).
--
-- B2 — Trust strip under the hero: homepage ordering is owned by
-- homepage_sections.sort_order (rendered by HomeFeedSections right after
-- the static HeroSlider + SpecialDealsSlider in App.tsx). trust_features
-- was seeded last (sort_order 220 in 202610160002; live prod: 140, the
-- final section). Move it to the FIRST feed slot so the trust guarantees
-- render directly under the hero/deals block. The relative MIN()-10 form
-- works regardless of how the live rows were reordered in the admin CMS.
-- Idempotent on manual re-run: the MIN is taken over the OTHER rows
-- (section_type <> 'trust_features'), so trust's own already-lowered
-- value never feeds back into the computation (no -10 drift per re-run).
UPDATE homepage_sections
SET sort_order = (SELECT COALESCE(MIN(sort_order), 0) - 10 FROM homepage_sections WHERE section_type <> 'trust_features'),
    updated_at = NOW()
WHERE section_type = 'trust_features';

-- B3 — Duplicate categories removed: the homepage keeps the top category
-- circles (CategoriesBar, static in App.tsx). The mid-page
-- suppliers_showcase section («غرفه‌ها و کالکشن‌های تخصصی») duplicates
-- that category navigation, so it leaves the homepage composition.
-- Deactivated (not deleted) so the row and its history stay intact and
-- the change is reversible from the admin CMS.
UPDATE homepage_sections
SET is_active = FALSE,
    updated_at = NOW()
WHERE section_type = 'suppliers_showcase';
