/**
 * Holding one element still while something is open on top of it.
 *
 * showModal() makes the rest of the document inert to focus and pointer targeting, not to the
 * wheel: a wheel over the backdrop scrolls whatever is underneath.
 *
 * Counted, because two dialogs can be open at once and the inner one closing must not hand back
 * the scroll while the outer is still up.
 */
type Held = { count: number; overflow: string; paddingRight: string };

const held = new Map<HTMLElement, Held>();

/**
 * Stops el scrolling until the returned function is called. A second call does nothing: React
 * runs cleanups again on re-runs, and twice on purpose in development.
 */
export function lockScroll(el: HTMLElement): () => void {
  const already = held.get(el);
  if (already) {
    already.count += 1;
  } else {
    // Read first: the gap is the scrollbar's width, and it is zero once overflow is hidden.
    const gap = scrollbarWidth(el);
    held.set(el, {
      count: 1,
      overflow: el.style.overflow,
      paddingRight: el.style.paddingRight,
    });
    el.style.overflow = "hidden";
    // Padding where the scrollbar was, so what is behind does not shift sideways.
    if (gap > 0) {
      const padding = Number.parseFloat(getComputedStyle(el).paddingRight) || 0;
      el.style.paddingRight = `${padding + gap}px`;
    }
  }

  let released = false;
  return () => {
    if (released) return;
    released = true;

    const lock = held.get(el);
    if (!lock) return;
    lock.count -= 1;
    if (lock.count > 0) return;

    held.delete(el);
    // Restored rather than cleared, so an element's own overflow survives.
    el.style.overflow = lock.overflow;
    el.style.paddingRight = lock.paddingRight;
  };
}

/** The page's scrollbar belongs to the viewport, so the body measured against itself is 0. */
function scrollbarWidth(el: HTMLElement): number {
  if (el === document.body || el === document.documentElement) {
    return window.innerWidth - document.documentElement.clientWidth;
  }
  return el.offsetWidth - el.clientWidth;
}
