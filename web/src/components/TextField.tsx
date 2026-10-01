import type { InputHTMLAttributes } from "react";

/** A field's frame, ground and focus. Width and text size belong to the caller. */
export function TextField({
  className = "",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`sunken min-h-10 rounded-md border-0 bg-bg px-3 py-1.5 sm:min-h-11 sm:py-2 text-fg placeholder:text-faint focus:outline-none ${className}`}
      {...props}
    />
  );
}
