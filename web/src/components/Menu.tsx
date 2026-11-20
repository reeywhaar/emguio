import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { PickerButton } from "@app/components/PickerButton";

/** How far an open menu keeps from the window's edges. */
const EDGE = 8;

/**
 * A button that opens a short list beside it: below it, for a bar at the top of the screen. It
 * closes on a choice, on Escape, and on a press anywhere else, which does nothing else.
 */
export function Menu({
  name,
  label,
  plain = false,
  chevron,
  look,
  end = false,
  title,
  disabled,
  className = "",
  children,
}: {
  /** What is being chosen, which a screen reader hears before the label. */
  name: string;
  label: ReactNode;
  /** Just the label, without a field around it: for a bar, not a form. */
  plain?: boolean;
  chevron?: boolean;
  /** A look of its own for the button, such as a button's. */
  look?: string;
  /** Opened along the button's right edge rather than its left: for a button at the right. */
  end?: boolean;
  title?: string;
  disabled?: boolean;
  /** The button's. */
  className?: string;
  /** The items, each given a way to close the menu once it has done its thing. */
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const list = useRef<HTMLDivElement>(null);

  // Moved back inside the window when its button is near an edge, as on a phone.
  useLayoutEffect(() => {
    const el = list.current;
    if (!open || !el) return;
    const { left, right } = el.getBoundingClientRect();
    const width = document.documentElement.clientWidth;
    const off =
      left < EDGE
        ? EDGE - left
        : right > width - EDGE
          ? width - EDGE - right
          : 0;
    el.style.translate = off ? `${off}px 0` : "";
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", escape);
    return () => document.removeEventListener("keydown", escape);
  }, [open]);

  return (
    <div className="relative min-w-0">
      <PickerButton
        name={name}
        plain={plain}
        chevron={chevron}
        look={look}
        title={title}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        className={className}
        onClick={() => setOpen((was) => !was)}
      >
        {label}
      </PickerButton>
      {open ? (
        // Over the whole page, under the list: a press outside lands here and nowhere else.
        <div
          data-veil
          aria-hidden="true"
          className="fixed inset-0 z-30"
          onClick={() => setOpen(false)}
        />
      ) : null}
      {open ? (
        <div
          ref={list}
          role="menu"
          className={`absolute top-full ${end ? "right-0" : "left-0"} z-40 mt-1.5 flex max-h-[70dvh] w-max max-w-[calc(100vw-2rem)] min-w-56 flex-col overflow-y-auto rounded-lg border border-line bg-bg p-1 shadow-lg`}
        >
          {children(() => setOpen(false))}
        </div>
      ) : null}
    </div>
  );
}

/** One line of a menu. */
export function MenuItem({
  danger,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { danger?: boolean }) {
  return (
    <button
      type="button"
      role="menuitem"
      className={`cursor-default rounded-md px-3 py-1.5 text-left text-sm whitespace-nowrap select-none hover:bg-shade disabled:opacity-50 disabled:hover:bg-transparent pointer-coarse:py-2.5 ${
        danger ? "text-accent" : "text-fg"
      }`}
      {...props}
    />
  );
}
