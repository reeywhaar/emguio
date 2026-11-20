import type { ComponentProps } from "react";

import { ChevronMark } from "@app/components/icons";
import { fieldLook } from "@app/components/TextField";

/** A picker button's look, for what has to be drawn as wide as one. */
export function pickerLook(plain: boolean) {
  const look = plain
    ? "gap-1 rounded-md px-2 hover:bg-fill"
    : `${fieldLook} gap-2`;
  return `${look} flex h-8 min-w-0 items-center text-left text-sm pointer-coarse:h-10`;
}

/**
 * A select's look, or plain, for a button that opens its list in a dialog: a list that holds
 * more than a native select can show, a tree, counts, a way to add another.
 */
export function PickerButton({
  name,
  plain = false,
  chevron = true,
  look,
  className = "",
  children,
  ...props
}: ComponentProps<"button"> & {
  /** What is being chosen, which a screen reader hears before what was chosen. */
  name: string;
  /** Just the choice, without the field around it: for a bar, not a form. */
  plain?: boolean;
  /** Off for a label that says it opens something itself. */
  chevron?: boolean;
  /** A look of its own in place of a select's or plain, such as a button's. */
  look?: string;
}) {
  return (
    <button
      type="button"
      aria-haspopup="dialog"
      className={`${look ?? pickerLook(plain)} ${className}`}
      {...props}
    >
      <span className="sr-only">{name}: </span>
      {children}
      {chevron ? <ChevronMark /> : null}
    </button>
  );
}
