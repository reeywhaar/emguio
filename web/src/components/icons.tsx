/**
 * The few marks a row needs, drawn inline so they take the text's color and need no request.
 * Named for what they say, which is what a screen reader reads.
 */

const box = {
  width: 14,
  height: 14,
  viewBox: "0 0 16 16",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.5,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export function Paperclip({ className = "" }: { className?: string }) {
  return (
    <svg {...box} className={className} role="img" aria-label="Attachment">
      <path d="M10.5 4.5 5.8 9.2a1.3 1.3 0 0 0 1.8 1.8l5-5a2.6 2.6 0 0 0-3.7-3.7l-5 5a3.9 3.9 0 0 0 5.5 5.5l4.1-4.1" />
    </svg>
  );
}

export function Star({ className = "" }: { className?: string }) {
  return (
    <svg
      {...box}
      fill="currentColor"
      className={className}
      role="img"
      aria-label="Flagged"
    >
      <path d="m8 1.8 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.6l-3.8 2 .7-4.3-3.1-3 4.3-.6Z" />
    </svg>
  );
}

export function Refresh({ className = "" }: { className?: string }) {
  return (
    <svg {...box} className={className} aria-hidden="true">
      <path d="M13.5 8A5.5 5.5 0 1 1 11.9 4.1M13.5 2.5v3h-3" />
    </svg>
  );
}
