import type { ReactNode } from "react";

/** A small mark beside text that says what something is, or is not. */
export function Tag({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={`inline-block shrink-0 self-center rounded bg-fill px-1 align-middle text-[0.6875rem] leading-4 font-normal text-muted ${className}`}
    >
      {children}
    </span>
  );
}
