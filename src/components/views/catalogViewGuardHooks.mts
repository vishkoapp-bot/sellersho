// Test-only module customization hooks for CatalogView.test.tsx.
// Redirects every import of the StoreContext module to the stub
// (catalogViewStoreStub.ts) — EXCEPT imports from the stub itself
// (which re-exports the real activeStoreSlugOf) and from the test file
// (which asserts on the real scope derivation). Registered with
// module.register() at the top of the test file, before CatalogView is
// dynamically imported. tsx normalises the extension-less specifier to
// '.../StoreContext.tsx' before it reaches this hook, hence the match
// below; the redirect is keyed on the importing file, so the test
// file's own direct import still resolves to the real module.
const stubUrl = new URL('./catalogViewStoreStub.ts', import.meta.url).href;

export async function resolve(
  specifier: string,
  context: { parentURL?: string },
  nextResolve: (specifier: string, context: unknown) => Promise<unknown>,
): Promise<unknown> {
  const parent = context.parentURL ?? '';
  const isStoreContext =
    specifier.endsWith('/context/StoreContext.tsx') || specifier.endsWith('/context/StoreContext.ts');
  const isExempt =
    parent.endsWith('/catalogViewStoreStub.ts') || parent.endsWith('/CatalogView.test.tsx');
  if (isStoreContext && !isExempt) {
    return { url: stubUrl, shortCircuit: true };
  }
  return nextResolve(specifier, context);
}
