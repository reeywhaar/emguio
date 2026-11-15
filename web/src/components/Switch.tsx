/**
 * Something on or off, flipped by a press: a button that says which as a switch does, so a
 * screen reader hears "on" or "off" rather than a label that changes.
 */
export function Switch({
  on,
  label,
  disabled = false,
  onChange,
}: {
  on: boolean;
  /** What is on or off, which a screen reader hears; shown as the tooltip too. */
  label: string;
  disabled?: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-50 ${on ? "bg-brand" : "bg-line"}`}
    >
      <span
        aria-hidden="true"
        className={`inline-block size-4 rounded-full bg-bg shadow-sm transition-transform ${on ? "translate-x-4.5" : "translate-x-0.5"}`}
      />
    </button>
  );
}
