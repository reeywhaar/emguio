/**
 * Dates for a person, in the viewer's own zone and words. The server never formats one.
 */

function sameDay(a: Date, b: Date) {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/** As short as it can be and still say when: a time today, a day this year, a date before. */
export function when(unix: number, now = new Date()): string {
  const d = new Date(unix * 1000);
  if (sameDay(d, now)) {
    return d.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }
  return d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/** The whole of it, for a title to show on hover. */
export function full(unix: number): string {
  return new Date(unix * 1000).toLocaleString(undefined, {
    dateStyle: "full",
    timeStyle: "short",
  });
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
