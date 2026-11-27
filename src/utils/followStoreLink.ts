import type { MouseEvent } from 'react';

export function followStoreLink(event: MouseEvent<HTMLAnchorElement>, navigate: () => void) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  navigate();
}

export function followStoreSection(event: MouseEvent<HTMLAnchorElement>) {
  const hash = event.currentTarget.hash;
  const destination = document.getElementById(hash.slice(1));
  if (!destination) return;
  followStoreLink(event, () => {
    window.history.replaceState(window.history.state, '', hash);
    destination.scrollIntoView({ block: 'start' });
    if (destination instanceof HTMLInputElement) destination.focus({ preventScroll: true });
  });
}
