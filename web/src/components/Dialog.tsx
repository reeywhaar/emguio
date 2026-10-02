import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";

import { Boundary } from "@app/components/Boundary";
import { Cross } from "@app/components/icons";
import { lockScroll } from "@app/components/scrollLock";

/**
 * The dialog a piece of the tree is inside, if any. A ref, because the provider is the dialog
 * and its element exists only after the first render; a child's effect reads it after that.
 */
const DialogContext = createContext<RefObject<HTMLDialogElement | null> | null>(
  null,
);

/** How long a dialog takes to fade in or out; main.css says the same. */
const FADE = 150;

/**
 * A modal on the native dialog element, at every size.
 *
 * Native, because showModal() brings focus trapping, Escape, an inert page behind and top-layer
 * stacking, each of which is easy to reimplement slightly wrong.
 *
 * Controlled: the element's open state is DOM state, and React's is the one that decides.
 */
export function Dialog({
  open,
  onClose,
  title,
  lead,
  aside,
  actions,
  children,
  footer,
  wide = false,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  /** Before the title, on its line: where what the dialog holds lives. */
  lead?: ReactNode;
  /** What the title is about, on the title's line rather than a row of its own. */
  aside?: ReactNode;
  /** Buttons in the title bar beside the close: another face of the same dialog. */
  actions?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  /** For a form with fields side by side. */
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const parent = useContext(DialogContext);

  // Kept while it fades out, then unmounted, so a form starts empty rather than holding what was
  // abandoned.
  const [shown, setShown] = useState(open);
  if (open && !shown) setShown(true);
  useEffect(() => {
    if (open) return;
    const gone = setTimeout(() => setShown(false), FADE);
    return () => clearTimeout(gone);
  }, [open]);

  // A click that began on text inside and ended on the backdrop targets the dialog too, and is
  // somebody selecting, not leaving.
  const startedOnBackdrop = useRef(false);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    // showModal on an open dialog throws, and close on a shut one fires a second close.
    if (open && !dialog.open) {
      dialog.showModal();
      // showModal focuses the first control whether it wanted focus or not, and a ring on a
      // destructive button reads as armed. A field that wants focus says so with
      // data-autofocus; React's autoFocus runs while the dialog is still hidden.
      const wants = dialog.querySelector("[data-autofocus]");
      if (wants instanceof HTMLElement) wants.focus();
      else dialog.focus();
    } else if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    // Both: holding the page still leaves a parent dialog free to scroll underneath, and the
    // reverse.
    const releasePage = lockScroll(document.body);
    const above = parent?.current;
    const releaseParent = above ? lockScroll(above) : undefined;
    return () => {
      releaseParent?.();
      releasePage();
    };
  }, [open, parent]);

  return (
    <dialog
      ref={ref}
      tabIndex={-1}
      // close and cancel do not bubble in the DOM, but React delivers them as though they did,
      // so a dialog opened inside this one would close this one too.
      onClose={(e) => e.target === ref.current && onClose()}
      // Escape fires cancel before close; one way out.
      onCancel={(e) => {
        if (e.target !== ref.current) return;
        e.preventDefault();
        onClose();
      }}
      onPointerDown={(e) => {
        startedOnBackdrop.current = e.target === e.currentTarget;
      }}
      onClick={(e) => {
        if (startedOnBackdrop.current && e.target === e.currentTarget) {
          onClose();
        }
        startedOnBackdrop.current = false;
      }}
      // hidden until open, or flex beats the browser's dialog:not([open]) { display: none }.
      // The whole screen on a phone, where margins would only show the page nobody is reading.
      // Above it a card: m-auto undoes the preflight's margin: 0, which would leave it in the
      // corner, and h-fit stops the two insets of a modal stretching it to the full height.
      className={`hidden h-dvh max-h-dvh w-dvw max-w-none flex-col overflow-hidden border-0 bg-bg p-0 text-fg backdrop:bg-black/40 focus:outline-none open:flex sm:m-auto sm:h-fit sm:max-h-[85dvh] sm:rounded-xl sm:border sm:border-line sm:shadow-xl ${
        wide
          ? "sm:w-[min(42rem,calc(100vw-2rem))]"
          : "sm:w-[min(28rem,calc(100vw-2rem))]"
      }`}
    >
      {shown ? (
        <DialogContext.Provider value={ref}>
          {/* Outside the scroller, so what the dialog holds always has its name above it. A
              close always drawn, because a phone has no Escape and no backdrop to press. */}
          <div className="flex shrink-0 items-center justify-between gap-3 border-b border-line px-4 py-2.5 sm:px-5">
            <div className="flex min-w-0 items-center gap-2">
              {lead}
              <h2 className="truncate text-base font-semibold">{title}</h2>
              {aside}
            </div>
            <div className="flex shrink-0 items-center gap-1.5">
              {actions}
              <button
                type="button"
                aria-label="Close"
                title="Close"
                onClick={onClose}
                className="-mr-1 shrink-0 rounded-md p-1 text-faint hover:bg-fill hover:text-fg"
              >
                <Cross />
              </button>
            </div>
          </div>

          {/* The body scrolls and the footer does not, so what the dialog asks for is never
              below the fold. flex-auto rather than flex-1: Safari sizes a flex-1 child of a
              fitted box at its basis of 0. overscroll-contain keeps a scroll that reached the
              end from carrying on into the page behind. */}
          <div className="flex min-h-0 flex-auto flex-col gap-4 overflow-y-auto overscroll-contain px-4 pt-4 sm:px-5">
            {/* Under the title bar, so a body that throws still leaves its close. */}
            <Boundary what="This">{children}</Boundary>
            {/* A box rather than padding: a flex scroll container drops its padding-bottom at
                the end of a scroll, and the last field would sit against the edge. */}
            <div aria-hidden="true" className="h-2 shrink-0" />
          </div>
          {footer ? (
            <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-line px-4 pt-4 pb-5 sm:px-5">
              {footer}
            </div>
          ) : null}
        </DialogContext.Provider>
      ) : null}
    </dialog>
  );
}
