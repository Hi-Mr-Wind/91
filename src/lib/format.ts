export function formatCount(n: number | undefined): string {
  if (n === undefined || n === null) return "0";
  if (n < 1000) return String(n);
  if (n < 10000) return `${(n / 1000).toFixed(1)}k`;
  if (n < 1_000_000) return `${(n / 10000).toFixed(1)}w`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}
