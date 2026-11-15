/**
 * Which of a row's items show when not all of them fit: those that do, in order, with a button
 * for the rest after them, and the current one always among them. All of them when they fit, and
 * none when not even the current one does beside the button.
 */
export function fit({
  ids,
  widths,
  room,
  button,
  gap,
  current,
}: {
  ids: string[];
  widths: Record<string, number>;
  room: number;
  /** The width of the button for the rest. */
  button: number;
  gap: number;
  current?: string;
}): string[] {
  const width = (id: string) => widths[id] ?? 0;
  const all = ids.reduce(
    (sum, id, i) => sum + width(id) + (i > 0 ? gap : 0),
    0,
  );
  if (all <= room) return ids;
  let left = room - button;
  const shown = new Set<string>();
  const take = (id: string) => {
    const w = width(id) + gap;
    if (w > left) return false;
    left -= w;
    shown.add(id);
    return true;
  };
  if (current !== undefined && ids.includes(current) && !take(current)) {
    return [];
  }
  for (const id of ids) {
    if (shown.has(id)) continue;
    if (!take(id)) break;
  }
  return ids.filter((id) => shown.has(id));
}
