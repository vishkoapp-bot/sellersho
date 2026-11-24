import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createRequire } from 'node:module';

// Vite owns this parser; resolve it relative to Vite for strict pnpm layouts.
const require = createRequire(import.meta.url);
const { parse } = createRequire(require.resolve('vite/package.json'))('postcss') as {
  parse(source: string): { walkRules(cb: (rule: {
    selector: string;
    parent?: { type: string; name: string; params: string };
    walkDecls(property: string, cb: (decl: { value: string }) => void): void;
  }) => void): void };
};

test('DRP-7: Fashion hero breakpoints and title sizing are relative to the storefront frame', () => {
  const css = parse(readFileSync(new URL('./fashion.css', import.meta.url), 'utf8'));
  const contexts = new Set<string>();
  let name: string | undefined;
  let type: string | undefined;
  let titleSize: string | undefined;
  css.walkRules(rule => {
    if (rule.selector === '.fashion-storefront') rule.walkDecls('container', decl => {
      [name, type] = decl.value.split(/\s*\/\s*/);
    });
    if (rule.selector === '.fashion-hero h1') rule.walkDecls('font-size', decl => { titleSize = decl.value; });
    if (!/\.fashion-(?:hero|eyebrow)/.test(rule.selector) || rule.parent?.type !== 'atrule') return;
    if (!/min-width/.test(rule.parent.params)) return;
    contexts.add(`${rule.parent.name} ${rule.parent.params}`);
  });
  assert.deepEqual([...contexts], ['container fashion-frame (min-width: 640px)', 'container fashion-frame (min-width: 1024px)']);
  assert.equal(name, 'fashion-frame');
  assert.equal(type, 'inline-size');
  assert.match(titleSize ?? '', /3\.4cqi/);
});
