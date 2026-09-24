/** "42s ago", "3h 12m ago": precise enough to tell stale from fresh. */
export function ago(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) {
    return '—';
  }
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (seconds < 60) {
    return `${seconds}s ago`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m ago`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 48) {
    return `${hours}h ${minutes % 60}m ago`;
  }
  return `${Math.floor(hours / 24)}d ago`;
}

/** A revision shortened for display: the algorithm and twelve hex digits. */
export function shortRevision(rev: string | undefined): string {
  if (!rev) {
    return '—';
  }
  const [alg, hex] = rev.includes(':') ? rev.split(':', 2) : ['', rev];
  return alg ? `${alg}:${hex.slice(0, 12)}` : hex.slice(0, 12);
}
