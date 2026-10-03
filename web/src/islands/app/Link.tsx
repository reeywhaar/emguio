import type { ComponentProps, MouseEvent } from "react";

import { go } from "@app/islands/app/route";

/**
 * A real link, so it can be opened in a new tab, copied and read aloud as one, that moves within
 * the island on a plain click.
 */
export function Link({
  href,
  onClick,
  ...props
}: ComponentProps<"a"> & { href: string }) {
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0) return;
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    go(href);
  };
  return <a href={href} onClick={click} {...props} />;
}
