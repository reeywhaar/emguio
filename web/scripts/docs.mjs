import { mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { marked } from "marked";

/**
 * Builds the API reference, /docs, from docs/api.md.
 *
 * Static HTML with the whole text in it: a program does one GET and reads the body, so a page
 * that needed JavaScript would look right in a browser and be empty to a reader that never runs
 * any.
 */
const root = fileURLToPath(new URL("..", import.meta.url));

// A checkout has docs/ beside web/; the image copies it to /docs.
async function findSource() {
  for (const path of [`${root}../docs/api.md`, "/docs/api.md"]) {
    try {
      return await readFile(path, "utf8");
    } catch {
      // Try the next.
    }
  }
  throw new Error("docs/api.md not found");
}
const source = await findSource();
const body = marked.parse(source, { async: false });

const html = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" media="(prefers-color-scheme: light)" content="#ffffff">
<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#111113">
<link rel="alternate" type="text/markdown" href="/docs.md">
<title>emguio API</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#18181b;--muted:#52525b;--line:#e4e4e7;--surface:#f7f7f8}
@media (prefers-color-scheme:dark){:root{--bg:#111113;--fg:#f4f4f5;--muted:#a1a1aa;--line:#27272a;--surface:#18181b}}
body{background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,sans-serif;margin:0}
main{max-width:46rem;margin:0 auto;padding:2rem 1rem 6rem}
h1,h2,h3{line-height:1.25}
h2{margin-top:2.5rem;border-top:1px solid var(--line);padding-top:1.5rem}
a{color:inherit}
code{background:var(--surface);padding:.1em .35em;border-radius:.25rem;font-size:.9em}
pre{background:var(--surface);padding:1rem;border-radius:.5rem;overflow-x:auto}
pre code{background:none;padding:0}
table{border-collapse:collapse;width:100%;font-size:.95em;display:block;overflow-x:auto}
th,td{border-bottom:1px solid var(--line);padding:.4rem .6rem;text-align:left;vertical-align:top}
blockquote{margin:1rem 0;padding-left:1rem;border-left:3px solid var(--line);color:var(--muted)}
.back{font-size:.9em;color:var(--muted)}
.block{position:relative}
.block button{position:absolute;top:.4rem;right:.4rem;font:inherit;font-size:.75rem;
 color:var(--muted);background:var(--bg);border:1px solid var(--line);border-radius:.375rem;
 padding:.2rem .5rem;cursor:pointer;opacity:0;transition:opacity .1s}
.block:hover button,.block button:focus-visible{opacity:1}
@media (pointer:coarse){.block button{opacity:1}}
</style>
</head>
<body>
<main>
<p class="back"><a href="/">&larr; emguio</a> &middot; <a href="/docs.md">this page as markdown</a></p>
${body}
</main>
<script>
/* An enhancement and nothing more: the page is the whole text without it, which matters because
   a program reading it does one GET and never runs any of this. */
for (const pre of document.querySelectorAll("pre")) {
  const box = document.createElement("div");
  box.className = "block";
  pre.replaceWith(box);
  const button = document.createElement("button");
  button.type = "button";
  button.title = "Copy this block";
  button.textContent = "Copy";
  /* Without the newline marked puts at the end: pasted into a shell, a trailing newline is the
     Return key, and the command runs before it has been read. */
  const text = (pre.textContent || "").replace(/\\n$/, "");
  button.addEventListener("click", async () => {
    if (!(await copy(text))) return;
    button.textContent = "Copied";
    setTimeout(() => (button.textContent = "Copy"), 1200);
  });
  box.append(pre, button);
}

/* navigator.clipboard is not there outside a secure context, which is any instance reached over
   plain http at something other than localhost. */
async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const box = document.createElement("textarea");
    box.value = text;
    box.readOnly = true;
    box.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
    document.body.append(box);
    box.select();
    try { return document.execCommand("copy"); }
    catch { return false; }
    finally { box.remove(); }
  }
}
</script>
</body>
</html>
`;

await mkdir(`${root}dist`, { recursive: true });
await writeFile(`${root}dist/docs.html`, html);
await writeFile(`${root}dist/docs.md`, source);
console.log("docs: dist/docs.html, dist/docs.md");
