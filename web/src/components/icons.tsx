import type { ReactNode } from "react";

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

export function Cross({ className = "" }: { className?: string }) {
  return (
    <svg
      {...box}
      width={18}
      height={18}
      className={className}
      aria-hidden="true"
    >
      <path d="m4 4 8 8M12 4l-8 8" />
    </svg>
  );
}

/** Marks for the reading pane's actions: silent, because each button says what it does. */
function Mark({
  children,
  filled = false,
}: {
  children: ReactNode;
  filled?: boolean;
}) {
  return (
    <svg
      {...box}
      width={16}
      height={16}
      fill={filled ? "currentColor" : "none"}
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const ArchiveMark = () => (
  <Mark>
    <path d="M2 3h12v3H2zM3 6v7h10V6M6.5 8.5h3" />
  </Mark>
);

export const TrashMark = () => (
  <Mark>
    <path d="M2.5 4h11M6 4V2.5h4V4M4 4l.7 9.5h6.6L12 4" />
  </Mark>
);

export const SpamMark = () => (
  <Mark>
    <path d="M5.5 1.5h5l4 4v5l-4 4h-5l-4-4v-5zM8 5v3.5M8 11h.01" />
  </Mark>
);

export const InboxMark = () => (
  <Mark>
    <path d="M1.5 9 3.5 3h9l2 6v4h-13zM1.5 9h4l1 2h3l1-2h4" />
  </Mark>
);

export const StarMark = ({ filled }: { filled: boolean }) => (
  <Mark filled={filled}>
    <path d="m8 1.8 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.6l-3.8 2 .7-4.3-3.1-3 4.3-.6Z" />
  </Mark>
);

export const MailMark = ({ open }: { open: boolean }) => (
  <Mark>
    {open ? (
      <path d="M1.5 7 8 2.5 14.5 7v6.5h-13zM1.5 7 8 11l6.5-4" />
    ) : (
      <path d="M1.5 3.5h13v9h-13zM1.5 3.5 8 8.5l6.5-5" />
    )}
  </Mark>
);

export const FolderMark = () => (
  <Mark>
    <path d="M1.5 3.5h4.5l1.5 1.5h7v8h-13zM7 9h4.5M9.5 7l2 2-2 2" />
  </Mark>
);
