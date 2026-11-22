import { apiRequest } from './apiClient';
import { fingerprintHash, newUuid } from './productModel';
let fingerprint: Promise<string> | undefined;
const browserProof = () => {
  const automated = navigator.webdriver || /Headless|Puppeteer|PhantomJS/i.test(navigator.userAgent)
    || ['__playwright__binding__', '__nightmare', 'callPhantom'].some((key) => key in window);
  fingerprint ??= (async () => {
    let visitor: string;
    try { visitor = localStorage.getItem('sellersho_ad_visitor') || newUuid(); localStorage.setItem('sellersho_ad_visitor', visitor); }
    catch { visitor = newUuid(); }
    const traits = [visitor, navigator.userAgent, navigator.language, screen.width, screen.height, screen.colorDepth, Intl.DateTimeFormat().resolvedOptions().timeZone, navigator.hardwareConcurrency];
    return fingerprintHash(JSON.stringify(traits));
  })();
  return { automated, fingerprint };
};
export async function recordVerifiedAdEvent(campaignId: string, type: 'impression' | 'click', token: string, productId?: string) {
  const browser = browserProof();
  const signals = { fingerprint: await browser.fingerprint, webdriver: navigator.webdriver, automation: browser.automated };
  let proof: { challenge: string; issuedAt?: number } | null = null;
  const cacheKey = `sellersho_ad_proof:${campaignId}`;
  try { const cached = JSON.parse(sessionStorage.getItem(cacheKey) || 'null'); if (cached?.issuedAt && Date.now() - cached.issuedAt < 240000) proof = cached; } catch { /* A missing session cache requires a new proof. */ }
  if (!proof) {
    proof = await apiRequest<{ challenge: string }>('/ads/challenge', { method: 'POST', body: JSON.stringify({ campaignId, ...signals }) });
    try { sessionStorage.setItem(cacheKey, JSON.stringify({ ...proof, issuedAt: Date.now() })); } catch { /* Tracking works without persistent storage. */ }
  }
  const result = await apiRequest<{ accepted: boolean; reason?: string }>('/ads/events', { method: 'POST', body: JSON.stringify({ campaignId, type, token: token || newUuid(), productId, challenge: proof.challenge, ...signals }) });
  if (result.reason === 'invalid_challenge' || result.reason === 'expired_challenge') { try { sessionStorage.removeItem(cacheKey); } catch { /* Optional cache. */ } }
  return result;
}
