// Live banners are served by GET /api/v1/banners (see services/bannersApi)
// and rendered by HeroSlider / HomeFeedSections. The previous hardcoded
// gradient mock slides (with Snapp CDN images) were removed in FIX 2 for
// live parity; the homepage hero no longer reads any mock banner data.
// This module is kept (empty) so the `export * from "./banners"` re-export
// in mockData.ts stays valid. It exports no banner data.
export {};
