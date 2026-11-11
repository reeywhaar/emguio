/**
 * Dates for a person, in the viewer's own zone: days as ISO 8601 writes them and times on a
 * 24-hour clock, whatever the locale would. The server never formats one.
 */

function sameDay(a: Date, b: Date) {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

const two = (n: number) => String(n).padStart(2, "0");

/** A day as ISO 8601 writes it, whatever the locale: 2026-03-04. */
export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
}

/** A time of day on a 24-hour clock, whatever the locale: 09:05. */
export function clock(d: Date): string {
  return `${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** A day of the week, short, in the reader's words: Wed. */
export function weekday(d: Date): string {
  return d.toLocaleDateString(undefined, { weekday: "short" });
}

/** As short as it can be and still say when: a time today, the day otherwise. */
export function when(unix: number, now = new Date()): string {
  const d = new Date(unix * 1000);
  return sameDay(d, now) ? clock(d) : isoDay(d);
}

/** The whole of it: Wed 2026-03-04 09:05. */
export function full(unix: number): string {
  const d = new Date(unix * 1000);
  return `${weekday(d)} ${isoDay(d)} ${clock(d)}`;
}

/** How long ago, in words: "just now", "5 minutes ago", "3 hours ago". */
export function ago(unix: number, now = new Date()): string {
  const seconds = Math.round(unix - now.getTime() / 1000);
  if (seconds > -45) return "just now";
  const words = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  for (const [unit, length] of [
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ] as const) {
    if (Math.abs(seconds) >= length) {
      return words.format(Math.round(seconds / length), unit);
    }
  }
  return words.format(seconds, "second");
}

/** A size in the unit that keeps it short. */
export function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
