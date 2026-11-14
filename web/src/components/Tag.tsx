import type { ReactNode } from "react";

// small sits beside text; badge matches an address's badge, which it stands beside.
const sizes = {
  small: "px-1 text-[0.6875rem] leading-4",
  badge: "px-1.5 text-xs leading-5",
} as const;

/** A small mark beside text that says what something is, or is not. */
export function Tag({
  children,
  size = "small",
  className = "",
}: {
  children: ReactNode;
  size?: keyof typeof sizes;
  className?: string;
}) {
  return (
    <span
      className={`inline-block shrink-0 self-center rounded bg-fill align-middle font-normal text-muted mix-blend-multiply dark:mix-blend-screen ${sizes[size]} ${className}`}
    >
      {children}
    </span>
  );
}
