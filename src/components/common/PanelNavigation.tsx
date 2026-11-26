import type { MouseEvent } from 'react';
import { ChevronDown, type LucideIcon } from 'lucide-react';

interface PanelNavigationItem<Section extends string> {
  readonly id: Section;
  readonly label: string;
  readonly icon: LucideIcon;
}

export interface PanelNavigationGroup<Section extends string> extends PanelNavigationItem<string> {
  readonly items: readonly PanelNavigationItem<Section>[];
}

export function PanelNavigation<Section extends string>({ groups, current, href, onNavigate, label }: {
  groups: readonly PanelNavigationGroup<Section>[];
  current?: Section;
  href: (section: Section) => string;
  onNavigate: (section: Section) => void;
  label: string;
}) {
  function renderItem({ id, label, icon: Icon }: PanelNavigationItem<Section>) {
    const active = id === current;
    return (
      <li key={id}>
        <a href={href(id)} data-section={id} aria-current={active ? 'page' : undefined}
          onClick={(event: MouseEvent<HTMLAnchorElement>) => {
            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
            event.preventDefault();
            onNavigate(id);
          }}
          className={`flex min-h-11 items-center gap-2.5 rounded-xl px-3 py-1.5 text-sm leading-6 outline-offset-2 focus-visible:outline-2 focus-visible:outline-brand ${active ? 'bg-brand-soft font-extrabold text-brand-deep' : 'font-medium text-slate-700 hover:bg-slate-100'}`}>
          <Icon aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
          <span className="min-w-0 flex-1">{label}</span>
          {active && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-brand-deep" />}
        </a>
      </li>
    );
  }

  return (
    <nav aria-label={label}>
      <ul className="space-y-1.5">
        {groups.map(({ id, label, items, icon: Icon }) => {
          if (items.length === 1) return renderItem(items[0]);
          const isCurrent = items.some(item => item.id === current);
          return (
            <li key={id}>
              <details key={String(isCurrent)} open={isCurrent} className="group rounded-xl">
                <summary className={`flex min-h-11 cursor-pointer list-none items-center gap-2.5 rounded-xl px-3 py-1.5 text-sm font-bold outline-offset-2 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden ${isCurrent ? 'text-brand-deep' : 'text-slate-800'}`}>
                  <Icon aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.75} />
                  <span className="min-w-0 flex-1">{label}</span>
                  <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
                </summary>
                <ul className="ms-5 mt-1 space-y-0.5 border-s border-slate-200 ps-2">{items.map(renderItem)}</ul>
              </details>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
