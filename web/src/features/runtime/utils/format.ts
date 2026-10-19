const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

export function formatBytes(bytes: number): string {
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${unit === 0 ? value : value.toFixed(value >= 100 ? 0 : 1)} ${UNITS[unit]}`;
}

// formatUptime renders the time since startedAt as its two largest units, e.g. "2d 3h" or "5m 12s".
export function formatUptime(startedAt: string, now: number = Date.now()): string {
  const started = Date.parse(startedAt);
  if (Number.isNaN(started)) return '—';
  const seconds = Math.max(0, Math.floor((now - started) / 1000));
  const parts: [number, string][] = [
    [Math.floor(seconds / 86400), 'd'],
    [Math.floor((seconds % 86400) / 3600), 'h'],
    [Math.floor((seconds % 3600) / 60), 'm'],
    [seconds % 60, 's'],
  ];
  const first = parts.findIndex(([n]) => n > 0);
  if (first === -1) return '0s';
  return parts
    .slice(first, first + 2)
    .map(([n, u]) => `${n}${u}`)
    .join(' ');
}
