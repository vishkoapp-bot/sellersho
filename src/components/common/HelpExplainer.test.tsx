import React from 'react';
import assert from 'node:assert/strict';
import { it } from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { HelpExplainer, HELP_OPEN_EVENT, HELP_POPOVER_Z, HELP_SHEET_BACKDROP_Z, resolveHelpPresentation, positionHelpPopover } from './HelpExplainer';
import { shippingExplainer } from './helpTexts';

// Cases 2–7 and open rendering in 8–9 live in HelpExplainer.browser.test.tsx
// on batch8Browser (pattern §8.2); this file needs no live DOM.
// Helpers below test decisions, not browser interactions or portal rendering.
it('case 1: SSR renders a closed, focusable help button without a portal', () => {
  const html = renderToStaticMarkup(<HelpExplainer text={shippingExplainer} />);
  assert.match(html, /<button[^>]*type="button"/);
  assert.match(html, /aria-label="توضیح ساده"/);
  assert.match(html, /aria-expanded="false"/);
  assert.match(html, /focus-visible:outline-2/);
  assert.doesNotMatch(html, /role="dialog"/);
  assert.ok(!html.includes(shippingExplainer));
});

it('case 8 (SSR portion only): title supplies the accessible label', () => {
  const html = renderToStaticMarkup(<HelpExplainer text={shippingExplainer} title="پرداخت آنلاین" />);
  assert.match(html, /aria-label="توضیح: پرداخت آنلاین"/);
  assert.match(html, /aria-expanded="false"/);
});

it('an explicit accessible label overrides the title-derived label', () => {
  const html = renderToStaticMarkup(<HelpExplainer text={shippingExplainer} title="پرداخت آنلاین" ariaLabel="توضیح ساده" />);
  assert.match(html, /aria-label="توضیح ساده"/);
});

it('case 10: the trigger has no native title attribute', () => {
  const html = renderToStaticMarkup(<HelpExplainer text={shippingExplainer} title="پرداخت آنلاین" />);
  const button = html.match(/<button\b[^>]*>/)?.[0];
  assert.ok(button);
  assert.doesNotMatch(button, /\stitle=/);
});

it('size and variant decisions respect the 200/201 and 300/301 boundaries', () => {
  assert.deepEqual(resolveHelpPresentation({ text: 'x'.repeat(200) }), { size: 'sm', variant: 'popover' });
  assert.deepEqual(resolveHelpPresentation({ text: 'x'.repeat(201) }), { size: 'md', variant: 'popover' });
  assert.deepEqual(resolveHelpPresentation({ text: 'x'.repeat(300) }), { size: 'md', variant: 'popover' });
  assert.deepEqual(resolveHelpPresentation({ text: 'x'.repeat(301) }), { size: 'md', variant: 'sheet' });
  const html = renderToStaticMarkup(<HelpExplainer text={'x'.repeat(301)} />);
  assert.match(html, /aria-expanded="false"/);
  assert.doesNotMatch(html, /role="dialog"/);
});

it('grouped lengths, paragraphs and bullets select a sheet while simple lines remain a popover', () => {
  assert.equal(resolveHelpPresentation({ text: 'x'.repeat(150), sections: [{ heading: 'x', body: 'x'.repeat(150) }] }).variant, 'sheet');
  assert.equal(resolveHelpPresentation({ text: 'one\n\ntwo' }).variant, 'sheet');
  assert.equal(resolveHelpPresentation({ text: '- one\n- two' }).variant, 'sheet');
  assert.equal(resolveHelpPresentation({ text: 'one\ntwo' }).variant, 'popover');
  assert.deepEqual(resolveHelpPresentation({ text: 'x', variant: 'sheet', size: 'md' }), { size: 'md', variant: 'sheet' });
  assert.equal(resolveHelpPresentation({ text: 'x'.repeat(301), variant: 'popover' }).variant, 'popover');
});

it('publishes the agreed single-open event and overlay layers', () => {
  assert.equal(HELP_OPEN_EVENT, 'help-explainer:open');
  assert.equal(HELP_POPOVER_Z, 10001);
  assert.equal(HELP_SHEET_BACKDROP_Z, 10000);
});

it('positions below in RTL, clamps both edges and flips near the bottom', () => {
  assert.deepEqual(positionHelpPopover({ left: 340, right: 364, top: 20, bottom: 44 }, 240, 120, 390, 800), { left: 124, top: 48 });
  assert.equal(positionHelpPopover({ left: 0, right: 24, top: 20, bottom: 44 }, 240, 120, 390, 800).left, 8);
  assert.equal(positionHelpPopover({ left: 380, right: 404, top: 20, bottom: 44 }, 240, 120, 390, 800).left, 142);
  assert.equal(positionHelpPopover({ left: 340, right: 364, top: 750, bottom: 774 }, 240, 120, 390, 800).top, 626);
});

it('placement overrides and LTR positioning retain the viewport clamp', () => {
  const trigger = { left: 100, right: 124, top: 300, bottom: 324 };
  assert.deepEqual(positionHelpPopover(trigger, 240, 120, 390, 400, 'below', false), { left: 100, top: 272 });
  assert.equal(positionHelpPopover(trigger, 240, 120, 390, 800, 'above').top, 176);
});

it('clamps below when neither side fits and below has more space (P1-3)', () => {
  const position = positionHelpPopover({ left: 100, right: 124, top: 180, bottom: 204 }, 200, 200, 390, 400);
  assert.equal(position.top, 192);
  assert.ok(position.top + 200 <= 400);
});
