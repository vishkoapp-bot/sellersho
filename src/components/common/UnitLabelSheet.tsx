import type { PrintLabelPayload } from '../../services/unitsApi';
import { renderCode128Svg, UNIT_CODE_PATTERN } from '../../vendor/code128';

export const LABEL_SIZE_MM = { width: 100, height: 60 } as const;

export function Code128Barcode({ value, height }: { value: string; height?: number }) {
  return (
    <div
      className="spncx-barcode"
      dir="ltr"
      dangerouslySetInnerHTML={{ __html: renderCode128Svg(value, { heightPx: height }) }}
    />
  );
}

interface UnitLabelSheetProps {
  units: PrintLabelPayload[];
  preview?: boolean;
  printable?: boolean;
  printCountByCode?: Readonly<Record<string, number>>;
}

export function UnitLabelSheet({ units, printCountByCode, preview = false, printable = true }: UnitLabelSheetProps) {
  return (
    <div className={printable ? 'spncx-print-sheet' : 'no-print'} dir="rtl" style={preview ? { display: 'block' } : undefined}>
      {units.map(unit => {
        const printCount = printCountByCode?.[unit.code] ?? 0;
        const variantName = unit.variant_name.trim();
        const valid = UNIT_CODE_PATTERN.test(unit.code) && unit.barcode_value === unit.code;
        return (
          <div
            className="spncx-label box-border border border-slate-200 bg-white p-[4mm] text-center"
            key={unit.code}
            style={{ width: `${LABEL_SIZE_MM.width}mm`, height: `${LABEL_SIZE_MM.height}mm` }}
          >
            {valid ? (
              <>
                <p className="text-xs text-slate-500">سلرشو</p>
                <p className="line-clamp-2 break-words text-base font-bold">{unit.title}</p>
                {variantName && variantName !== 'پیش‌فرض' && (
                  <p className="truncate text-sm text-slate-600">{variantName}</p>
                )}
                <Code128Barcode value={unit.barcode_value} />
                <code className="block font-mono text-base font-bold" dir="ltr">{unit.code}</code>
                <p className="flex justify-center gap-4 font-mono text-xs" dir="ltr">
                  <span>SPN {unit.spn}</span>
                  <span>SPNC {unit.spnc}</span>
                </p>
                {printCount > 0 && <p className="text-xs text-slate-500">چاپ مجدد — {printCount} بار چاپ شده</p>}
              </>
            ) : (
              <p role="alert" className="text-sm font-bold text-red-700">قالب کد واحد درست نیست</p>
            )}
          </div>
        );
      })}
    </div>
  );
}
