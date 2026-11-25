import React, { useEffect, useRef } from 'react';

type ProbeLocation = Pick<Location, 'pathname' | 'search'>;

// Opt-in so the live deploy check does not interrupt regular shoppers.
export function GitDeployProbe({
  location = typeof window === 'undefined' ? undefined : window.location,
}: { location?: ProbeLocation }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const enabled = location?.pathname === '/' &&
    new URLSearchParams(location.search).get('git-deploy') === '1';

  useEffect(() => {
    if (enabled && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [enabled]);

  if (!enabled) return null;
  return (
    <dialog ref={dialog} aria-labelledby="git-deploy-title"
      className="m-auto rounded-xl border border-slate-200 bg-white p-6 text-center shadow-xl backdrop:bg-black/35">
      <h2 id="git-deploy-title" dir="ltr" className="text-lg font-semibold">git deploy</h2>
      <form method="dialog" className="mt-4">
        <button type="submit" className="rounded-lg bg-slate-900 px-4 py-2 text-white">بستن</button>
      </form>
    </dialog>
  );
}
