import React from 'react';

interface Props {
  data: number[];
  color?: string;
  height?: number;
  ariaLabel?: string;
}

// Minimal SVG trend line matching the live chart's empty-state contract:
// fewer than 2 points, or a max of 0, renders a dashed baseline instead
// of a line.
export const Sparkline: React.FC<Props> = ({ data, color = '#5d44f8', height = 38, ariaLabel = 'نمودار روند' }) => {
  const width = 220;
  const pad = 3;
  if (data.length < 2 || Math.max(...data) <= 0) {
    return (
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full" style={{ height }} role="img" aria-label={ariaLabel}>
        <line x1={0} y1={height - pad} x2={width} y2={height - pad} stroke="#e2e8f0" strokeWidth={1.5} strokeDasharray="3 3" />
      </svg>
    );
  }
  const max = Math.max(...data);
  const x = (i: number) => pad + (i / (data.length - 1)) * (width - pad * 2);
  const y = (v: number) => height - pad - (v / max) * (height - pad * 2);
  const points = data.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full" style={{ height }} role="img" aria-label={ariaLabel}>
      <polyline fill="none" stroke={color} strokeWidth={1.5} points={points} />
    </svg>
  );
};
