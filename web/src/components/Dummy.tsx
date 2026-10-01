/**
 * A grey rectangle standing in for something that is not here yet.
 *
 * Shaped like what it is waiting for, so the section does not change size when the answer
 * arrives. Hidden from anything reading the page aloud: a screen reader announcing blank
 * rectangles is worse than silence.
 */
export function Dummy({ className = "" }: { className?: string }) {
  return <div aria-hidden="true" className={`dummy rounded-md ${className}`} />;
}
