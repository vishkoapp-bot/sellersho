import { useEffect, useState, type ReactNode, type Ref } from 'react';
import { ArrowUpLeft, Bell, LogOut, Menu } from 'lucide-react';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { PanelNavigation, type PanelNavigationGroup } from './PanelNavigation';

export function PanelWorkspace<Section extends string>({
  id, label, name, title, current, groups, href, onNavigate, onBackToSite, onLogout,
  onOpenNotifications, unreadCount, formatCount, mainRef, headerActions, sidebarActions,
  mobileActions, modal, children,
}: {
  id: 'seller' | 'supplier';
  label: string;
  name: string;
  title: string;
  current?: Section;
  groups: readonly PanelNavigationGroup<Section>[];
  href: (section: Section) => string;
  onNavigate: (section: Section) => void;
  onBackToSite: () => void;
  onLogout: () => void;
  onOpenNotifications: () => void;
  unreadCount: number;
  formatCount: (count: number) => string;
  mainRef: Ref<HTMLElement>;
  headerActions?: ReactNode;
  sidebarActions?: ReactNode;
  mobileActions?: ReactNode;
  modal?: ReactNode;
  children: ReactNode;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const menuTitle = `${id}-menu-title`;
  const pageTitle = `${id}-page-title`;
  const mainId = `${id}-main`;

  useEffect(() => {
    const closeMenu = () => setMenuOpen(false);
    const desktop = window.matchMedia('(min-width: 1024px)');
    const closeOnDesktop = (event: MediaQueryListEvent) => { if (event.matches) closeMenu(); };
    window.addEventListener('popstate', closeMenu);
    desktop.addEventListener('change', closeOnDesktop);
    return () => {
      window.removeEventListener('popstate', closeMenu);
      desktop.removeEventListener('change', closeOnDesktop);
    };
  }, []);

  return (
    <div data-panel-workspace={id} dir="rtl" className="flex min-h-screen flex-1 flex-col bg-[#f7f7fa] text-slate-900">
      <a href={`#${mainId}`} className="sr-only focus:not-sr-only focus:fixed focus:start-4 focus:top-2 focus:z-40 focus:rounded-lg focus:bg-white focus:p-3 focus:text-brand-deep focus:outline-2 focus:outline-brand">رفتن به محتوای پنل</a>
      <header className="sticky top-0 z-30 border-b border-slate-200 bg-white">
        <div className="mx-auto flex min-h-16 w-full max-w-[1480px] items-center justify-between gap-2 px-3 sm:px-6">
          <div className="flex min-w-0 items-center gap-2 sm:gap-3">
            {groups.length > 0 && (
              <button type="button" onClick={() => setMenuOpen(true)} aria-label="باز کردن منوی پنل" aria-haspopup="dialog" aria-expanded={menuOpen}
                className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-xl bg-brand-soft px-2.5 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand lg:hidden">
                <Menu aria-hidden="true" className="size-5" strokeWidth={1.8} /> منو
              </button>
            )}
            <div className="min-w-0 py-2">
              <p className="truncate text-xs leading-5 text-slate-600" title={name}>{name}</p>
              <h1 id={pageTitle} className="break-words text-sm font-extrabold leading-6 sm:text-lg">{title}</h1>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1 sm:gap-2">
            {headerActions && <div className="hidden items-center gap-2 lg:flex">{headerActions}</div>}
            <button type="button" onClick={onOpenNotifications}
              aria-label={unreadCount ? `اعلان‌ها، ${formatCount(unreadCount)} خوانده‌نشده` : 'اعلان‌ها'} aria-haspopup="dialog"
              className="relative grid size-11 place-items-center rounded-xl text-slate-700 hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand">
              <Bell aria-hidden="true" className="size-5" strokeWidth={1.75} />
              {unreadCount > 0 && (
                <span aria-hidden="true" className="absolute -start-0.5 -top-0.5 grid min-h-5 min-w-5 place-items-center rounded-full bg-rose-700 px-1 text-[10px] font-bold text-white ring-2 ring-white">{formatCount(unreadCount)}</span>
              )}
            </button>
            <button type="button" onClick={onLogout} aria-label="خروج از حساب"
              className="hidden min-h-11 items-center gap-2 rounded-xl px-3 text-sm font-bold text-slate-700 hover:bg-rose-50 hover:text-rose-800 focus-visible:outline-2 focus-visible:outline-rose-700 lg:flex">
              <LogOut aria-hidden="true" className="size-4" strokeWidth={1.75} /> خروج
            </button>
          </div>
        </div>
      </header>

      {menuOpen && !modal && (
        <SupplierDialog titleId={menuTitle} onDismiss={() => setMenuOpen(false)}>
          <SupplierDialogHeading id={menuTitle} title="بخش‌های پنل" onClose={() => setMenuOpen(false)} />
          {mobileActions && <div className="mb-3 border-b border-slate-200 pb-3">{mobileActions}</div>}
          <PanelNavigation groups={groups} current={current} href={href} label={`منوی موبایل ${label}`} onNavigate={section => {
            setMenuOpen(false);
            onNavigate(section);
          }} />
          <div className="mt-4 flex flex-wrap gap-2 border-t border-slate-200 pt-2">
            <button type="button" onClick={onBackToSite} className="inline-flex min-h-11 items-center gap-2 rounded-xl px-3 text-sm font-bold text-slate-700 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand">
              سایت اصلی <ArrowUpLeft aria-hidden="true" className="size-4" />
            </button>
            <button type="button" onClick={onLogout} className="min-h-11 rounded-xl px-3 text-sm font-bold text-rose-800 hover:bg-rose-50 focus-visible:outline-2 focus-visible:outline-rose-700">خروج از حساب</button>
          </div>
        </SupplierDialog>
      )}

      <div className="mx-auto flex w-full max-w-[1480px] flex-1 flex-col gap-4 px-3 pb-8 pt-4 sm:px-6 lg:flex-row lg:gap-6 lg:pt-6">
        {groups.length > 0 && (
          <aside className="hidden w-[245px] shrink-0 self-start overflow-y-auto overscroll-contain rounded-2xl border border-slate-200 bg-white p-2.5 lg:sticky lg:top-[88px] lg:block lg:max-h-[calc(100dvh-108px)] xl:w-[258px]">
            <PanelNavigation groups={groups} current={current} href={href} label={`منوی پنل ${label}`} onNavigate={onNavigate} />
            <div className="mt-3 space-y-1 border-t border-slate-200 pt-2">
              {sidebarActions}
              <button type="button" onClick={onBackToSite} className="flex min-h-11 w-full items-center justify-between gap-2 rounded-xl px-3 text-sm font-bold text-slate-700 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand">
                سایت اصلی <ArrowUpLeft aria-hidden="true" className="size-4" />
              </button>
            </div>
          </aside>
        )}
        <main ref={mainRef} id={mainId} aria-labelledby={pageTitle} tabIndex={-1} className="min-w-0 flex-1 scroll-mt-24 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
          {children}
        </main>
      </div>
      {modal}
    </div>
  );
}
