import React from 'react';

export function RouteLoading({ label, screen = false }: { label: string; screen?: boolean }) {
  return (
    <div dir="rtl" className={`route-loading${screen ? ' route-loading--screen' : ''}`} role="status">
      <span className="route-loading-icon" aria-hidden="true"><span /></span>
      <span>{label}</span>
    </div>
  );
}
