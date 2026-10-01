import type { InputHTMLAttributes } from "react";

/** The look every typed-into control shares, for a select to wear as well. */
export const fieldLook =
  "min-h-10 rounded-md border border-line bg-bg px-3 py-1.5 text-fg placeholder:text-faint focus:border-brand focus:outline-none disabled:opacity-50";

/** A field's frame, ground and focus. Width belongs to the caller. */
export function TextField({
  className = "",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`${fieldLook} ${className}`} {...props} />;
}
