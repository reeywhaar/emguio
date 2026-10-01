import { useCallback, useEffect, useRef } from "react";

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
// The origin by name as well as 'self': Firefox does not count a srcdoc frame's inherited origin
// as 'self', and blocks every image in it, the message's own included.
const policy = (origin: string) =>
  `default-src 'none'; img-src 'self' ${origin} data:; style-src 'unsafe-inline'; font-src data:`;

// The frame fills the space under the headers, edge to edge, and scrolls itself: a mail lays
// itself out to fill a page. Taller than its space and left to the pane to scroll, it could not
// be scrolled on a phone, because Mobile Safari does not hand a touch on a frame to the pane
// around it.
const frame = (html: string) =>
  `<!doctype html><html><head><meta charset="utf-8">` +
  `<meta http-equiv="Content-Security-Policy" content="${policy(window.location.origin)}">` +
  `<base target="_blank">` +
  `<style>html{color-scheme:light;overflow-x:hidden;overflow-y:auto}` +
  `body{margin:0;font:14px/1.5 system-ui,sans-serif;color:#1f2329;background:#fff;` +
  `overflow-wrap:anywhere;width:fit-content;min-width:100%;box-sizing:border-box}` +
  `img{max-width:100%;height:auto}pre{white-space:pre-wrap}</style></head><body>${html}</body></html>`;

/**
 * images swaps each remote image's proxy address, which the server puts in data-src, in for the
 * blank that holds its place.
 */
export function Frame({ html, images }: { html: string; images: boolean }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const shown = useRef(images);
  shown.current = images;
  const watch = useRef<ResizeObserver | null>(null);

  /*
   * Fits the message to the frame's width. Mail is laid out for a fixed width — a 600px table is
   * the norm — and browsers will not shrink a table below the width it states, so one wider than
   * the frame is zoomed out until it fits, the way a phone's mail app does, rather than cut off
   * or scrolled sideways.
   */
  const fit = useCallback(() => {
    const doc = ref.current?.contentDocument;
    if (!doc?.body) return;
    const root = doc.documentElement;
    root.style.zoom = "";
    const natural = doc.body.scrollWidth;
    const room = root.clientWidth;
    if (natural > room && room > 0) root.style.zoom = String(room / natural);
  }, []);

  // Images arrive after the frame says it has loaded and can widen what they sit in, and the
  // pane can change width: either can need another fit, so the content is watched rather than
  // fitted once.
  const loaded = useCallback(() => {
    const doc = ref.current?.contentDocument;
    if (!doc?.body) return;
    if (shown.current) show(doc);
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

  useEffect(() => {
    const doc = ref.current?.contentDocument;
    if (images && doc?.body) show(doc);
  }, [images]);

  return (
    <iframe
      ref={ref}
      title="Message"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      srcDoc={frame(html)}
      onLoad={loaded}
      // A pixel wide with the pane's width as its minimum: Mobile Safari sizes an iframe to its
      // content and ignores a width it is given, but honors a minimum.
      className="block min-h-64 w-px min-w-full flex-1 border-0 bg-white"
    />
  );
}

function show(doc: Document) {
  for (const img of doc.querySelectorAll<HTMLImageElement>("img[data-src]")) {
    img.src = img.dataset.src!;
    img.removeAttribute("data-src");
  }
}
