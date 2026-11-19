import type { ReactNode } from "react";

/** One setting: what it is, a line on what it does, and its control at the right. */
export function Choice({
  title,
  note,
  children,
}: {
  title: string;
  note: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm">{title}</p>
        <p className="text-xs text-muted">{note}</p>
      </div>
      {children}
    </div>
  );
}
