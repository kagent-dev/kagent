/** Localized absolute datetime (e.g. "2026/5/21 10:35:00"). Returns "-" when missing/invalid. */
export function formatDateTime(dateStr?: string): string {
  if (!dateStr) return "-";
  const d = new Date(dateStr);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString();
}
