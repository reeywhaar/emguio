import { useEffect, useRef, useState, type ReactNode } from "react";

import { PickerButton } from "@app/components/PickerButton";

/**
 * A button that opens a short list beside it: below it, for a bar at the top of the screen. It
 * closes on a choice, on Escape, and on a press anywhere else.
 */
export function Menu({
  name,
  label,
  plain = false,
  disabled,
  className = "",
  children,
}: {
  /** What is being chosen, which a screen reader hears before the label. */
  name: string;
  label: ReactNode;
  /** Just the label, without a field around it: for a bar, not a form. */
  plain?: boolean;
  disabled?: boolean;
  /** The button's. */
  className?: string;
  /** The items, each given a way to close the menu once it has done its thing. */
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", away);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", away);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);

  return (
    <div ref={box} className="relative min-w-0">
      <PickerButton
        name={name}
        plain={plain}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        className={className}
        onClick={() => setOpen((was) => !was)}
      >
        {label}
      </PickerButton>
      {open ? (
        <div
          role="menu"
          className="absolute top-full left-0 z-40 mt-1.5 flex max-h-[70dvh] w-max max-w-[calc(100vw-2rem)] min-w-56 flex-col overflow-y-auto rounded-lg border border-line bg-bg p-1 shadow-lg"
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
      className={`rounded-md px-3 py-1.5 text-left text-sm whitespace-nowrap hover:bg-shade disabled:opacity-50 pointer-coarse:py-2.5 ${
        danger ? "text-accent" : "text-fg"
      }`}
      {...props}
    />
  );
}
