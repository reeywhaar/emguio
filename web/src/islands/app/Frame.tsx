import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Where a message's HTML is shown: a frame that runs no script and loads nothing from elsewhere.
 *
 * The server has sanitized the HTML already; this is the second wall, standing on its own. The
 * sandbox gives no allow-scripts, so nothing in the message runs whatever the sanitizer missed.
 * allow-same-origin is there only so this page can measure the frame — without scripts of its
 * own the frame has no use for it. The CSP lets images come from this origin alone, which is the
 * message's own parts and the image proxy, so a tracking pixel the sanitizer missed still loads
 * nothing.
 *
 * Always on white: mail is written for a white page, and a dark theme forced onto somebody
 * else's colors makes half of it unreadable.
 */
const policy =
  "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; font-src data:";

// The frame never scrolls: the reading pane around it does, and two scrollbars on one message
// is one too many. Its height is the content's, measured.
const frame = (html: string) =>
  `<!doctype html><html><head><meta charset="utf-8">` +
  `<meta http-equiv="Content-Security-Policy" content="${policy}">` +
  `<base target="_blank">` +
  `<style>html{color-scheme:light;overflow:hidden}` +
  `body{margin:0;padding:16px;font:14px/1.5 system-ui,sans-serif;color:#1f2329;background:#fff;` +
  `overflow-wrap:anywhere;width:fit-content;min-width:100%;box-sizing:border-box}` +
  `img{max-width:100%;height:auto}pre{white-space:pre-wrap}</style></head><body>${html}</body></html>`;

export function Frame({ html }: { html: string }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const watch = useRef<ResizeObserver | null>(null);
  const [height, setHeight] = useState(240);

  /*
   * Fits the message to the frame. Mail is laid out for a fixed width — a 600px table is the
   * norm — and browsers will not shrink a table below the width it states, so one wider than the
   * frame is zoomed out until it fits, the way a phone's mail app does, rather than cut off or
   * scrolled sideways. Then the frame takes the height of what it now holds.
   */
  const fit = useCallback(() => {
    const el = ref.current;
    const doc = el?.contentDocument;
    if (!el || !doc?.body) return;
    const root = doc.documentElement;
    root.style.zoom = "";
    const natural = doc.body.scrollWidth;
    const room = el.clientWidth;
    const zoom = natural > room && room > 0 ? room / natural : 1;
    if (zoom < 1) root.style.zoom = String(zoom);
    // Plus the frame's own border, which the height it is given includes.
    const border = el.offsetHeight - el.clientHeight;
    setHeight(Math.ceil(doc.body.getBoundingClientRect().height) + border);
  }, []);

  // Images arrive after the frame says it has loaded, and the pane can narrow when its own
  // scrollbar appears: either changes the height, so the content is watched rather than
  // measured once.
  const loaded = useCallback(() => {
    const doc = ref.current?.contentDocument;
    if (!doc?.body) return;
    fit();
    doc.addEventListener("load", fit, true);
    watch.current?.disconnect();
    if (typeof ResizeObserver !== "undefined") {
      watch.current = new ResizeObserver(() => fit());
      watch.current.observe(doc.body);
      watch.current.observe(ref.current!);
    }
  }, [fit]);

  useEffect(() => () => watch.current?.disconnect(), []);

  return (
    <iframe
      ref={ref}
      title="Message"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      srcDoc={frame(html)}
      onLoad={loaded}
      scrolling="no"
      style={{ height }}
      className="block w-full overflow-hidden rounded-md border border-line bg-white"
    />
  );
}
