import React, { useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { buildAdBannerLink, buildStoreReferralLink } from '../../utils/attributionTokens';
import { Link2, QrCode, Copy, Check, Share2 } from 'lucide-react';
import { SellerCampaign, storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

// ---------------------------------------------------------------------------
// Minimal QR encoder (byte mode, error-correction level M), self-contained:
// the candidate tree has no QR library (no qrcode dependency anywhere in
// the worktree), while live renders a real QR canvas. This encoder follows
// the ISO/IEC 18004 placement rules (finder/timing/alignment patterns,
// format + version info, zig-zag data mapping, mask evaluation) and was
// verified module-for-module against a reference encoder for versions 1-10.
// It exists only so the tab can render and download a real, scannable QR
// code exactly like live does, without adding a dependency.
// ---------------------------------------------------------------------------
const QR_ALIGN: number[][] = [
  [], [6, 18], [6, 22], [6, 26], [6, 30], [6, 34],
  [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50],
];
// [totalCodewords, ecPerBlock, group1Count, group1Data, group2Count, group2Data] for level M.
const QR_BLOCKS_M: number[][] = [
  [26, 10, 1, 16, 0, 0], [44, 16, 1, 28, 0, 0], [70, 26, 1, 44, 0, 0],
  [100, 18, 2, 32, 0, 0], [134, 24, 2, 43, 0, 0], [172, 16, 4, 27, 0, 0],
  [196, 18, 4, 31, 0, 0], [242, 22, 2, 38, 2, 39], [292, 22, 3, 36, 2, 37],
  [346, 26, 4, 43, 1, 44],
];
const QR_EXP = new Array<number>(512);
const QR_LOG = new Array<number>(256);
{
  let x = 1;
  for (let i = 0; i < 255; i++) { QR_EXP[i] = x; QR_LOG[x] = i; x <<= 1; if (x & 0x100) x ^= 0x11d; }
  for (let i = 255; i < 512; i++) QR_EXP[i] = QR_EXP[i - 255];
}
const qrMul = (a: number, b: number): number => (a === 0 || b === 0 ? 0 : QR_EXP[QR_LOG[a] + QR_LOG[b]]);
const qrPolyMul = (p: number[], q: number[]): number[] => {
  const out = new Array<number>(p.length + q.length - 1).fill(0);
  for (let i = 0; i < p.length; i++) for (let j = 0; j < q.length; j++) out[i + j] ^= qrMul(p[i], q[j]);
  return out;
};
const qrGenerator = (degree: number): number[] => {
  let poly = [1];
  for (let i = 0; i < degree; i++) poly = qrPolyMul(poly, [1, QR_EXP[i]]);
  return poly;
};
const qrRemainder = (data: number[], degree: number): number[] => {
  const gen = qrGenerator(degree);
  const out = [...data, ...new Array<number>(degree).fill(0)];
  for (let i = 0; i < data.length; i++) {
    const coef = out[i];
    if (coef === 0) continue;
    for (let j = 0; j < gen.length; j++) out[i + j] ^= qrMul(gen[j], coef);
  }
  return out.slice(data.length);
};
const qrBchDigit = (n: number): number => (n === 0 ? 0 : Math.floor(Math.log2(n)) + 1);
const qrFormatBits = (mask: number): number => {
  // EC level M = 0b00; G15 = 0x537, mask 0x5412.
  const data = mask;
  let d = data << 10;
  const g15 = 0x537;
  while (qrBchDigit(d) - qrBchDigit(g15) >= 0) d ^= g15 << (qrBchDigit(d) - qrBchDigit(g15));
  return ((data << 10) | d) ^ 0x5412;
};
const qrVersionBits = (version: number): number => {
  let d = version << 12;
  const g18 = 0x1f25;
  while (qrBchDigit(d) - qrBchDigit(g18) >= 0) d ^= g18 << (qrBchDigit(d) - qrBchDigit(g18));
  return (version << 12) | d;
};
const qrMaskApplies = (mask: number, row: number, col: number): boolean => {
  switch (mask) {
    case 0: return (row + col) % 2 === 0;
    case 1: return row % 2 === 0;
    case 2: return col % 3 === 0;
    case 3: return (row + col) % 3 === 0;
    case 4: return (Math.floor(row / 2) + Math.floor(col / 3)) % 2 === 0;
    case 5: return (row * col) % 2 + (row * col) % 3 === 0;
    case 6: return ((row * col) % 2 + (row * col) % 3) % 2 === 0;
    default: return ((row * col) % 3 + (row + col) % 2) % 2 === 0;
  }
};
const qrPenalty = (m: boolean[][]): number => {
  const n = m.length;
  let score = 0;
  for (let r = 0; r < n; r++) {
    let run = 1;
    for (let c = 1; c < n; c++) {
      if (m[r][c] === m[r][c - 1]) { run++; } else { if (run >= 5) score += 3 + (run - 5); run = 1; }
    }
    if (run >= 5) score += 3 + (run - 5);
  }
  for (let c = 0; c < n; c++) {
    let run = 1;
    for (let r = 1; r < n; r++) {
      if (m[r][c] === m[r - 1][c]) { run++; } else { if (run >= 5) score += 3 + (run - 5); run = 1; }
    }
    if (run >= 5) score += 3 + (run - 5);
  }
  for (let r = 0; r < n - 1; r++) for (let c = 0; c < n - 1; c++) {
    if (m[r][c] === m[r + 1][c] && m[r][c] === m[r][c + 1] && m[r][c] === m[r + 1][c + 1]) score += 3;
  }
  const pat1 = [true, false, true, true, true, false, true, false, false, false, false];
  const pat2 = [false, false, false, false, true, false, true, true, true, false, true];
  const matchAt = (line: boolean[], at: number, pat: boolean[]) => pat.every((v, k) => line[at + k] === v);
  for (let r = 0; r < n; r++) for (let c = 0; c + 11 <= n; c++) if (matchAt(m[r], c, pat1) || matchAt(m[r], c, pat2)) score += 40;
  for (let c = 0; c < n; c++) {
    const col = m.map((row) => row[c]);
    for (let r = 0; r + 11 <= n; r++) if (matchAt(col, r, pat1) || matchAt(col, r, pat2)) score += 40;
  }
  let dark = 0;
  for (const row of m) for (const v of row) if (v) dark++;
  const percent = (dark * 100) / (n * n);
  score += Math.floor(Math.abs(percent - 50) / 5) * 10;
  return score;
};

export const encodeQrMatrix = (text: string): boolean[][] => {
  const bytes = Array.from(new TextEncoder().encode(text));
  let version = 0;
  for (let v = 1; v <= 10; v++) {
    const [total, ec, g1n, g1d, g2n, g2d] = QR_BLOCKS_M[v - 1];
    const dataCw = g1n * g1d + g2n * g2d;
    const countBits = v < 10 ? 8 : 16;
    if (4 + countBits + bytes.length * 8 <= dataCw * 8) { version = v; break; }
    void total; void ec;
  }
  if (!version) throw new Error('data too long');
  const [totalCw, ecPerBlock, g1n, g1d, g2n, g2d] = QR_BLOCKS_M[version - 1];
  const dataCwCount = g1n * g1d + g2n * g2d;
  const countBits = version < 10 ? 8 : 16;
  const bits: number[] = [];
  const put = (value: number, length: number) => { for (let i = length - 1; i >= 0; i--) bits.push((value >> i) & 1); };
  put(0b0100, 4);
  put(bytes.length, countBits);
  for (const b of bytes) put(b, 8);
  const capacity = dataCwCount * 8;
  for (let i = 0; i < Math.min(4, capacity - bits.length); i++) bits.push(0);
  while (bits.length % 8 !== 0) bits.push(0);
  const dataBytes: number[] = [];
  for (let i = 0; i < bits.length; i += 8) dataBytes.push(bits.slice(i, i + 8).reduce((acc, bit) => (acc << 1) | bit, 0));
  for (let i = 0; dataBytes.length < dataCwCount; i++) dataBytes.push(i % 2 === 0 ? 0xec : 0x11);
  // Split into blocks, compute EC, interleave.
  const blocks: number[][] = [];
  let offset = 0;
  for (let g = 0; g < g1n; g++) { blocks.push(dataBytes.slice(offset, offset + g1d)); offset += g1d; }
  for (let g = 0; g < g2n; g++) { blocks.push(dataBytes.slice(offset, offset + g2d)); offset += g2d; }
  const ecBlocks = blocks.map((b) => qrRemainder(b, ecPerBlock));
  const finalBytes: number[] = [];
  const maxData = Math.max(g1d, g2d);
  for (let i = 0; i < maxData; i++) for (const b of blocks) if (i < b.length) finalBytes.push(b[i]);
  for (let i = 0; i < ecPerBlock; i++) for (const e of ecBlocks) finalBytes.push(e[i]);
  void totalCw;
  // Function modules.
  const size = 21 + 4 * (version - 1);
  const modules: (boolean | null)[][] = Array.from({ length: size }, () => new Array<boolean | null>(size).fill(null));
  const set = (r: number, c: number, v: boolean) => { if (r >= 0 && r < size && c >= 0 && c < size) modules[r][c] = v; };
  const finder = (r0: number, c0: number) => {
    for (let r = -1; r < 8; r++) for (let c = -1; c < 8; c++) {
      const rr = r0 + r; const cc = c0 + c;
      if (rr < 0 || rr >= size || cc < 0 || cc >= size) continue;
      const dark = (r >= 0 && r <= 6 && (c === 0 || c === 6)) || (c >= 0 && c <= 6 && (r === 0 || r === 6)) || (r >= 2 && r <= 4 && c >= 2 && c <= 4);
      set(rr, cc, dark);
    }
  };
  finder(0, 0); finder(0, size - 7); finder(size - 7, 0);
  // Order matters and follows the reference implementation: alignment
  // patterns are drawn BEFORE timing. A pattern centred on the timing row
  // or column (e.g. (6,24) in version 8) is therefore drawn in full, and
  // timing then skips its already-set cells. Drawing timing first and
  // skipping such patterns entirely produces a different (still scannable,
  // but non-reference) matrix.
  const align = QR_ALIGN[version - 1];
  for (const r of align) for (const c of align) {
    if (modules[r][c] !== null) continue;
    for (let dr = -2; dr <= 2; dr++) for (let dc = -2; dc <= 2; dc++) {
      set(r + dr, c + dc, dr === -2 || dr === 2 || dc === -2 || dc === 2 || (dr === 0 && dc === 0));
    }
  }
  for (let i = 8; i < size - 8; i++) { if (modules[i][6] === null) set(i, 6, i % 2 === 0); if (modules[6][i] === null) set(6, i, i % 2 === 0); }
  const applyFormat = (mask: number, test: boolean) => {
    const bits15 = test ? 0 : qrFormatBits(mask);
    for (let i = 0; i < 15; i++) {
      const bit = test ? false : ((bits15 >> i) & 1) === 1;
      if (i < 6) set(i, 8, bit);
      else if (i < 8) set(i + 1, 8, bit);
      else set(size - 15 + i, 8, bit);
      if (i < 8) set(8, size - i - 1, bit);
      else if (i < 9) set(8, 15 - i - 1 + 1, bit);
      else set(8, 15 - i - 1, bit);
    }
    set(size - 8, 8, !test);
  };
  const applyVersion = (test: boolean) => {
    if (version < 7) return;
    const bits18 = test ? 0 : qrVersionBits(version);
    for (let i = 0; i < 18; i++) {
      const bit = test ? false : ((bits18 >> i) & 1) === 1;
      set(Math.floor(i / 3), (i % 3) + size - 8 - 3, bit);
      set((i % 3) + size - 8 - 3, Math.floor(i / 3), bit);
    }
  };
  const mapData = (mask: number) => {
    let inc = -1; let row = size - 1; let bitIndex = 7; let byteIndex = 0;
    // Column pairs follow the reference traversal exactly: the pair list is
    // fixed first (size-1, size-3, ..., 2), and a pair at/below column 6 is
    // shifted down by one — so the leftmost pairs are (5,4), (3,2), (1,0).
    // Iterating a plain step-by-two loop with an in-body decrement skips
    // the (3,2) pair and mis-assigns bits in the leftmost columns.
    const colValues: number[] = [];
    for (let c = size - 1; c > 0; c -= 2) colValues.push(c);
    for (const rawCol of colValues) {
      const col = rawCol <= 6 ? rawCol - 1 : rawCol;
      const cols = [col, col - 1];
      for (;;) {
        for (const c of cols) {
          if (modules[row][c] === null) {
            let dark = false;
            if (byteIndex < finalBytes.length) dark = ((finalBytes[byteIndex] >> bitIndex) & 1) === 1;
            if (qrMaskApplies(mask, row, c)) dark = !dark;
            modules[row][c] = dark;
            bitIndex--;
            if (bitIndex === -1) { byteIndex++; bitIndex = 7; }
          }
        }
        row += inc;
        if (row < 0 || row >= size) { row -= inc; inc = -inc; break; }
      }
    }
  };
  const snapshot = (): boolean[][] => modules.map((row) => row.map((v) => v === true));
  // Evaluate masks on a copy with format modules blanked (reference behaviour).
  applyVersion(false);
  const funcWithVersion: (boolean | null)[][] = modules.map((row) => [...row]);
  let bestMask = 0; let bestScore = Infinity;
  for (let mask = 0; mask < 8; mask++) {
    for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) modules[r][c] = funcWithVersion[r][c];
    applyFormat(mask, true);
    mapData(mask);
    const score = qrPenalty(snapshot());
    if (mask === 0 || score < bestScore) { bestScore = score; bestMask = mask; }
  }
  for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) modules[r][c] = funcWithVersion[r][c];
  applyFormat(bestMask, false);
  mapData(bestMask);
  return snapshot();
};

// Live parity (SellerDashboardView-MERGED.js, links tab vn, offsets
// ~171150-175300): live's link/QR logic is the same attribution helpers
// the candidate already uses (buildAdBannerLink / buildStoreReferralLink),
// but every label, toast and the QR flow itself differed. Ported verbatim:
// intro banner, "لینک فروشگاهت" card, "این لینک را کجا می‌گذاری؟"
// options, "کپی"/"کپی شد" button, banner warning, real QR canvas for the
// plain store URL (no campaign params) with the sellershow.ir/store/<slug>
// caption, and a real PNG download named qr-<slug>.png. The previous
// candidate version showed a fake "QR: <slug>" box and only toasted that
// a file "was ready" without downloading anything.
export const SellerLinksTab: React.FC = () => {
  const { currentStore } = useStore();
  const { success, error } = useToast();
  const [copiedLink, setCopiedLink] = useState(false);
  const [campaignTag, setCampaignTag] = useState('instagram-bio');
  const [campaigns, setCampaigns] = useState<SellerCampaign[]>([]);
  const qrCanvasRef = useRef<HTMLCanvasElement | null>(null);
  useEffect(() => { if (!isMockMode()) storeApi.getSellerCampaigns(currentStore.slug).then(setCampaigns).catch(() => setCampaigns([])); }, [currentStore.slug]);

  const isBannerAd = campaignTag === 'website-banner';
  const selectedCampaign = campaigns.find((campaign) => campaign.type === (isBannerAd ? 'banner' : 'link') && campaign.status === 'active');
  const storeUrl = `https://sellershow.ir/store/${currentStore.slug}`;
  const mainStoreLink = selectedCampaign
    ? `${buildAdBannerLink(storeUrl, currentStore.slug, campaignTag)}&campaignId=${selectedCampaign.id}`
    : isBannerAd
    ? buildAdBannerLink(storeUrl, currentStore.slug, 'website_banner')
    : buildStoreReferralLink(currentStore.slug, campaignTag);

  const copyToClipboard = () => {
    navigator.clipboard.writeText(mainStoreLink);
    setCopiedLink(true);
    success('لینک فروشگاهت کپی شد؛ هر جا خواستی بفرستش.');
    setTimeout(() => setCopiedLink(false), 2000);
  };

  // Live encodes the PLAIN store URL in the QR (not the campaign link).
  useEffect(() => {
    const canvas = qrCanvasRef.current;
    if (!canvas || !currentStore.slug) return;
    try {
      const matrix = encodeQrMatrix(storeUrl);
      const size = matrix.length;
      const modulePx = Math.max(1, Math.floor(512 / size));
      canvas.width = size * modulePx;
      canvas.height = size * modulePx;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.fillStyle = '#000000';
      for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) {
        if (matrix[r][c]) ctx.fillRect(c * modulePx, r * modulePx, modulePx, modulePx);
      }
    } catch {
      error('رندر QR انجام نشد؛ لطفاً دوباره تلاش کنید.');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentStore.slug]);

  const handleDownloadQr = () => {
    const canvas = qrCanvasRef.current;
    if (!canvas) { error('رندر QR انجام نشد؛ لطفاً دوباره تلاش کنید.'); return; }
    const link = document.createElement('a');
    link.href = canvas.toDataURL('image/png');
    link.download = `qr-${currentStore.slug}.png`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    success(`تصویر QR فروشگاه «${currentStore.name}» دانلود شد.`);
  };

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="لینک و کد QR" description="لینک فروشگاه را برای مشتری بفرست یا کد QR را برای چاپ دانلود کن." icon={<Link2 />} />

      <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(18rem,0.65fr)]">
        <section className="min-w-0 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <h3 className="text-base font-extrabold text-[#261b41]">لینک فروشگاهت</h3>

          <div>
            <label htmlFor="seller-link-channel" className="mb-1 block text-sm font-bold text-slate-700">این لینک را کجا می‌گذاری؟</label>
            <select
              id="seller-link-channel"
              value={campaignTag}
              onChange={(e) => setCampaignTag(e.target.value)}
              className="min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 text-sm focus-visible:ring-2 focus-visible:ring-brand"
            >
              <option value="instagram-bio">اینستاگرام (در بیو پیج)</option>
              <option value="telegram-channel">کانال تلگرام</option>
              <option value="whatsapp-direct">واتساپ و پیام‌رسان‌ها</option>
              <option value="website-banner">بنر در سایت خودم</option>
            </select>
          </div>

          <div className="flex items-center gap-2 p-2 bg-slate-50 rounded-xl border border-slate-200" dir="ltr">
            <input
              type="text"
              aria-label="لینک اشتراک‌گذاری فروشگاه"
              readOnly
              value={mainStoreLink}
              className="min-w-0 flex-1 rounded-lg bg-transparent px-1 py-2 font-mono text-xs text-slate-700 focus-visible:ring-2 focus-visible:ring-brand"
            />
            <button
              type="button"
              onClick={copyToClipboard}
              className="flex min-h-11 shrink-0 items-center gap-1 rounded-lg bg-brand-deep px-3 text-xs font-bold text-white focus-visible:ring-2 focus-visible:ring-brand"
            >
              {copiedLink ? <Check className="w-4 h-4" strokeWidth={1.5} /> : <Copy className="w-4 h-4" strokeWidth={1.5} />}
              <span>{copiedLink ? 'کپی شد' : 'کپی'}</span>
            </button>
          </div>
          {!selectedCampaign && isBannerAd && <p className="text-[10px] text-amber-700">برای اینکه کلیک‌های بنرت شمرده شود، اول در بخش «تبلیغات» یک برنامه تبلیغاتی از نوع بنر بساز.</p>}
        </section>

        {/* 2. QR Code */}
        <section className="min-w-0 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-center shadow-sm sm:p-5">
          <div className="flex items-center justify-center gap-2 text-brand-deep">
            <QrCode aria-hidden="true" className="size-4" strokeWidth={1.5} />
            <h3 className="text-base font-extrabold text-[#261b41]">کد QR فروشگاهت</h3>
          </div>
          <p className="text-sm leading-6 text-slate-600">مشتری با اسکن این کد وارد فروشگاه می‌شود.</p>

          <div className="w-40 h-40 mx-auto bg-white border border-slate-200 rounded-2xl flex items-center justify-center p-2 shadow-inner">
            <canvas ref={qrCanvasRef} style={{ width: 144, height: 144 }} aria-label={`QR فروشگاه ${currentStore.name}`} />
          </div>
          <button
            type="button"
            onClick={handleDownloadQr}
            className="mx-auto flex min-h-11 items-center justify-center gap-1.5 rounded-xl border border-brand-border px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand"
          >
            <Share2 className="w-4 h-4" strokeWidth={1.5} />
            <span>دانلود کد QR برای چاپ</span>
          </button>
        </section>
      </div>
    </div>
  );
};
