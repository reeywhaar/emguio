import { useRef, type PointerEvent as Press } from "react";

/** How far a press has to travel to be a drag rather than a press that wobbled. */
const SLOP = 6;

/** How long a finger rests on a row before picking it up; moving sooner scrolls. */
const HOLD = 400;

/**
 * Press, carry, drop.
 *
 * Pointer events rather than mouse or touch ones, so a finger, a pen and a mouse all reach it
 * once. The element being dragged captures the pointer, because it is a small target among
 * others and the pointer leaves it on the first move; what is under the pointer is then asked
 * for by hit-testing rather than by listening, which is the same question asked of the position
 * the capture still reports.
 *
 * A finger has to rest first: the rows sit in a list it scrolls, and a finger that moves at
 * once is scrolling it. Once it has rested, hold keeps the list still under it.
 *
 * Callers keep their own state for what is being carried and where it would land. This owns the
 * part that is fiddly and identical: when a press becomes a drag, what it is over, and whether
 * the click that follows should be swallowed — a press is one intention, and a finger lifting
 * off should not also open the thing it has just moved.
 */
export function useCarry({
  find,
  onStart,
  onOver,
  onDrop,
  enabled = true,
}: {
  /** What counts as somewhere to land: a selector its targets match. */
  find: string;
  /** The press has travelled far enough, or rested long enough, to be a drag. */
  onStart?: () => void;
  /** What the pointer is over now, or nothing. */
  onOver: (target: HTMLElement | null) => void;
  /** Let go of, after a drag. */
  onDrop: () => void;
  /** Off where there is nothing to arrange, so a press is only ever a press. */
  enabled?: boolean;
}) {
  const from = useRef<{ x: number; y: number } | null>(null);
  const moving = useRef(false);
  /** This press has already meant something, so the click after it means nothing. */
  const spent = useRef(false);
  const resting = useRef<ReturnType<typeof setTimeout>>(undefined);

  const over = (x: number, y: number) => {
    const under = document.elementFromPoint(x, y)?.closest(find);
    onOver(under instanceof HTMLElement ? under : null);
  };
  const start = () => {
    moving.current = true;
    spent.current = true;
    onStart?.();
  };
  const still = (e: TouchEvent) => {
    if (moving.current) e.preventDefault();
  };
  const end = (dropped: boolean) => {
    clearTimeout(resting.current);
    from.current = null;
    if (moving.current && dropped) onDrop();
    moving.current = false;
  };

  return {
    spent,
    /** A ref for the element pressed: a finger carrying it does not scroll what is under it. */
    hold: (el: HTMLElement | null) => {
      if (!el) return;
      // Not React's: it listens passively, and a passive listener cannot keep anything still.
      el.addEventListener("touchmove", still, { passive: false });
      return () => el.removeEventListener("touchmove", still);
    },
    press: (e: Press<HTMLElement>) => {
      spent.current = false;
      moving.current = false;
      clearTimeout(resting.current);
      from.current = null;
      if (!enabled || e.button !== 0) return;
      const at = { x: e.clientX, y: e.clientY };
      from.current = at;
      if (e.pointerType === "touch") {
        resting.current = setTimeout(() => {
          start();
          over(at.x, at.y);
        }, HOLD);
      } else {
        e.currentTarget.setPointerCapture(e.pointerId);
      }
    },
    move: (e: Press<HTMLElement>) => {
      const at = from.current;
      if (!at || !enabled) return;
      if (!moving.current) {
        if (Math.hypot(e.clientX - at.x, e.clientY - at.y) < SLOP) return;
        // Moved before it rested: a scroll, which is the list's.
        if (e.pointerType === "touch") return end(false);
        start();
      }
      over(e.clientX, e.clientY);
    },
    release: () => end(true),
    cancel: () => end(false),
    /** A finger resting long enough to carry is not asking for the menu a long press opens. */
    menu: (e: { preventDefault: () => void }) => {
      if (moving.current) e.preventDefault();
    },
  };
}
