export function shortId(id: string): string {
  return `${id.slice(0, 8)}…${id.slice(-4)}`;
}
