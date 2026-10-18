// Builds the site for GitHub Pages: a copy of site/ plus the user docs from
// docs/wiki/*.md rendered into site pages under docs/.
//
//   node scripts/docs/build.mjs [outDir]     (default: out/site)
//
// Home.md becomes docs/index.html, Quick-Start.md docs/quick-start.html, and
// so on; _Sidebar.md is the page list, in order. Links between pages are
// rewritten to the .html pages, links out of docs/wiki/ point at the file on
// GitHub, and a link to a page that doesn't exist fails the build.
// Markdown is rendered by the vendored marked (vendor/VERSIONS); no npm install.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Marked, Renderer } from "./vendor/marked.esm.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const src = path.join(root, "docs", "wiki");
const out = path.resolve(process.argv[2] || path.join(root, "out", "site"));
const REPO = "https://github.com/varogonz95/clawsh";

// The newest stable release (highest vX.Y.Z tag), shown as the last item of
// every page's main nav in place of <!--release-->. Needs the tags fetched (pages.yml
// checks out with fetch-depth 0); without any, the label is left out.
function latestRelease() {
  let tags = "";
  try {
    tags = execFileSync("git", ["-C", root, "tag", "-l", "v*"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
  } catch {}
  const versions = tags.split("\n").filter((t) => /^v\d+\.\d+\.\d+$/.test(t)).map((t) => t.slice(1).split(".").map(Number));
  versions.sort((a, b) => a[0] - b[0] || a[1] - b[1] || a[2] - b[2]);
  return versions.length ? `v${versions.at(-1).join(".")}` : "";
}
const release = latestRelease();
const releaseLabel = release
  ? `<a class="release" href="${REPO}/releases/tag/${release}" title="Latest release">${release}</a>`
  : "";
const withRelease = (html) => html.replaceAll("<!--release-->", releaseLabel);

const slugOf = (file) => (file === "Home.md" ? "index" : path.basename(file, ".md").toLowerCase());
const esc = (s) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);

const files = fs.readdirSync(src).filter((f) => f.endsWith(".md") && !f.startsWith("_")).sort();
const errors = [];
const anchors = []; // [from, file, id]: checked once every page's heading ids are known
const ids = {}; // file -> Set of heading ids

// Page order and names come from the sidebar.
const sidebar = [...fs.readFileSync(path.join(src, "_Sidebar.md"), "utf8").matchAll(/\[([^\]]+)\]\(([^)]+\.md)\)/g)]
  .map(([, name, file]) => ({ name, file }));
for (const f of files) if (!sidebar.some((s) => s.file === f)) errors.push(`_Sidebar.md doesn't list ${f}`);

// GitHub-style heading ids, so #anchors written for GitHub keep working.
function headingId(text, used) {
  const base = text.toLowerCase().replace(/<[^>]+>/g, "").replace(/[^\p{L}\p{N} _-]/gu, "").trim().replace(/ /g, "-");
  let id = base, n = 0;
  while (used.has(id)) id = `${base}-${++n}`;
  used.add(id);
  return id;
}

function render(file) {
  const used = (ids[file] = new Set());
  let title = "";
  const marked = new Marked({
    gfm: true,
    renderer: {
      heading({ tokens, depth, text }) {
        const html = this.parser.parseInline(tokens);
        if (depth === 1 && !title) title = text;
        const id = headingId(text, used);
        return `<h${depth} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true" tabindex="-1">#</a>${html}</h${depth}>\n`;
      },
      link({ href, title: t, tokens }) {
        return `<a href="${esc(rewrite(href, file))}"${t ? ` title="${esc(t)}"` : ""}>${this.parser.parseInline(tokens)}</a>`;
      },
      table(token) {
        // Wide tables scroll inside their own box on small screens.
        return `<div class="table">${Renderer.prototype.table.call(this, token)}</div>\n`;
      },
    },
  });
  const html = marked.parse(fs.readFileSync(path.join(src, file), "utf8"));
  return { html, title: title || path.basename(file, ".md") };
}

// Rewrites a link found in docs/wiki/<from> for the built site.
function rewrite(href, from) {
  if (href.startsWith("#")) {
    anchors.push([from, from, href.slice(1)]);
    return href;
  }
  if (/^([a-z]+:|\/\/)/i.test(href)) return href;
  const [p, hash = ""] = href.split("#");
  const target = path.posix.normalize(path.posix.join("docs/wiki", p));
  const suffix = hash ? `#${hash}` : "";
  if (target.startsWith("docs/wiki/") && target.endsWith(".md")) {
    const file = target.slice("docs/wiki/".length);
    if (!files.includes(file)) errors.push(`${from}: link to missing page ${href}`);
    else if (hash) anchors.push([from, file, hash]);
    return `${slugOf(file)}.html${suffix}`.replace(/^index\.html/, "./");
  }
  // Anything else in the repo (../PLAN.md, source files): the file on GitHub.
  if (!fs.existsSync(path.join(root, target))) errors.push(`${from}: link to missing file ${href}`);
  return `${REPO}/blob/main/${target}${suffix}`;
}

function page({ file, title, html }) {
  const nav = sidebar.map((s) =>
    `<li><a href="${s.file === "Home.md" ? "./" : `${slugOf(s.file)}.html`}"${s.file === file ? ' aria-current="page"' : ""}>${esc(s.name)}</a></li>`).join("\n          ");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} · clawsh docs</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Atkinson+Hyperlegible:ital,wght@0,400;0,700;1,400&family=Bagel+Fat+One&family=Martian+Mono:wdth,wght@87.5,400;87.5,600&display=swap" rel="stylesheet">
<link rel="stylesheet" href="../base.css">
<link rel="stylesheet" href="../docs.css">
<script>document.documentElement.classList.add("js")</script>
<script src="../nav.js" defer></script>
</head>
<body>

<header class="wrap top">
  <a class="mark" href="../" aria-label="clawsh home">claw<span class="sh" aria-hidden="true">sh</span></a>
  <nav aria-label="Main">
    <a href="../#web">Web UI</a>
    <a href="../#install">Install</a>
    <a href="../cli.html">CLI</a>
    <a href="./" aria-current="page">Docs</a>
    <a href="${REPO}">GitHub</a>
    <!--release-->
  </nav>
</header>

<div class="wrap docs">
  <nav class="pages" aria-label="Docs pages">
    <details>
      <summary>Pages</summary>
      <ul>
          ${nav}
      </ul>
    </details>
  </nav>
  <main class="prose">
${html}
    <p class="edit"><a href="${REPO}/edit/main/docs/wiki/${file}">Edit this page on GitHub</a></p>
  </main>
</div>

<footer>
  <div class="wrap">
    <span><span class="mark">claw<span class="sh">sh</span></span> &nbsp;A cross-machine manager for Claude Code. Not affiliated with Anthropic.</span>
    <span><a href="./">Docs</a> &nbsp; <a href="${REPO}/blob/main/LICENSE">License</a> &nbsp; <a href="${REPO}">GitHub</a></span>
  </div>
</footer>

<script>
  // Wide screens show the page list as an open sidebar; narrow ones keep it collapsed.
  if (matchMedia("(min-width: 880px)").matches) document.querySelector(".pages details").open = true;
</script>
</body>
</html>
`;
}

const pages = files.map((file) => ({ file, ...render(file) }));
for (const [from, file, id] of anchors) {
  if (!ids[file]?.has(id)) errors.push(`${from}: link to missing heading ${file}#${id}`);
}
if (errors.length) {
  console.error("docs build failed:\n  " + errors.join("\n  "));
  process.exit(1);
}

fs.rmSync(out, { recursive: true, force: true });
fs.cpSync(path.join(root, "site"), out, { recursive: true });
fs.mkdirSync(path.join(out, "docs"), { recursive: true });
for (const f of fs.readdirSync(out).filter((f) => f.endsWith(".html"))) {
  const file = path.join(out, f);
  fs.writeFileSync(file, withRelease(fs.readFileSync(file, "utf8")));
}
for (const p of pages) fs.writeFileSync(path.join(out, "docs", `${slugOf(p.file)}.html`), withRelease(page(p)));
console.log(`built ${out}: site + ${pages.length} docs pages, release ${release || "none"}`);
