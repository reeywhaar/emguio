import type { ButtonHTMLAttributes } from "react";

/**
 * Four variants, and choosing between them is the whole decision.
 *
 * solid is the brand color, which is the one thing on a screen of greys that is this
 * application rather than the browser's furniture. quiet is filled rather than outlined:
 * bordered, it draws a second rounded rectangle inside the one a panel already draws.
 */
type Variant = "solid" | "quiet" | "link" | "danger";

/**
 * How tall, which is a question about what the button is standing next to.
 *
 * field is beside an input and matches it, so both shrink together on a phone. bar is a row of
 * controls with no field in it, where a field's height reads as a stack of slabs — and where a
 * finger is still a finger, so the height comes back below a pointer that is not one.
 */
type Size = "field" | "bar";

const shapes: Record<Size, string> = {
  field: "min-h-10 min-w-20 px-3 py-1.5 text-sm sm:min-h-11",
  bar: "min-h-9 px-3 py-1.5 text-sm pointer-coarse:min-h-10",
};

const styles: Record<Variant, string> = {
  solid: "raised wash",
  quiet: "raised bg-bg text-muted hover:text-fg",
  link: "text-muted underline-offset-2 hover:text-fg hover:underline",
  danger: "raised bg-bg text-accent",
};

/** A label ends in "…" when the command stops to ask, and not when it acts on the press. */
export function Button({
  variant = "quiet",
  size = "field",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant;
  size?: Size;
}) {
  const shape =
    variant === "link"
      ? "text-sm"
      : `${shapes[size]} justify-center rounded-md`;
  return (
    <button
      type="button"
      className={`inline-flex items-center gap-1.5 disabled:opacity-50 ${shape} ${styles[variant]} ${className}`}
      {...props}
    />
  );
}
