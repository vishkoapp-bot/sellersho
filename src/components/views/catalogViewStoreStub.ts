// Test-only stand-in for ../../context/StoreContext, installed by
// catalogViewGuardHooks.mts so CatalogView (and the ProductCards it
// renders) can be rendered with renderToStaticMarkup without the real
// provider tree: the real StoreProvider installs currentStore only in
// effects, which never run under SSR, and useStore() throws when no
// store is selected. The stub re-exports the REAL activeStoreSlugOf so
// the scope derivation under test is the production one — the stub
// only fakes the *value* useStore() returns.
import type { useStore as RealUseStore } from '../../context/StoreContext';
import { activeStoreSlugOf } from '../../context/StoreContext';

export { activeStoreSlugOf };

// StoreContextType is not exported by the module, so the fake store
// value is typed off the exported useStore's return type instead.
export type FakeStoreValue = ReturnType<typeof RealUseStore>;

// Mutable holder for the fake store value. Tests set it before each
// render; renderToStaticMarkup is synchronous, so mutating it between
// renders is safe.
let fakeStore: FakeStoreValue | null = null;

export const setFakeStore = (store: FakeStoreValue): void => {
  fakeStore = store;
};

export const useStore = (): FakeStoreValue => {
  if (!fakeStore) {
    throw new Error('catalogViewStoreStub: setFakeStore() was not called before render');
  }
  return fakeStore;
};
