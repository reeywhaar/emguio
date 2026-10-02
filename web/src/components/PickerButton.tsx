import type { ComponentProps } from "react";

import { ChevronMark } from "@app/components/icons";
import { fieldLook } from "@app/components/TextField";

/**
 * A select's look, for a button that opens its list in a dialog: a list that holds more than
 * a native select can show, a tree, counts, a way to add another.
 */
export function PickerButton({
  name,
  className = "",
  children,
  ...props
}: ComponentProps<"button"> & {
  /** What is being chosen, which a screen reader hears before what was chosen. */
  name: string;
}) {
  return (
    <button
      type="button"
      aria-haspopup="dialog"
      className={`${fieldLook} flex h-8 min-w-0 items-center gap-2 text-left text-sm pointer-coarse:h-10 ${className}`}
      {...props}
    >
      <span className="sr-only">{name}: </span>
      {children}
      <ChevronMark />
    </button>
  );
}
