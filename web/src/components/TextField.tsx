import type { InputHTMLAttributes } from "react";

/** The look every typed-into control shares, for a select to wear as well. */
export const fieldLook =
  "rounded-md bg-control px-3 text-fg placeholder:text-faint focus:ring-1 focus:ring-brand focus:outline-none disabled:opacity-50";

/**
 * How tall, the same question a button's size answers: field among fields, bar in a row of
 * controls, where a finger still gets its height back.
 */
export const fieldSizes = {
  field: "min-h-10 py-1.5",
  bar: "min-h-8 py-1 text-sm pointer-coarse:min-h-10",
} as const;

/** A field's frame, ground and focus. Width belongs to the caller. */
export function TextField({
  className = "",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`${fieldLook} ${fieldSizes.field} ${className}`}
      {...props}
    />
  );
}
