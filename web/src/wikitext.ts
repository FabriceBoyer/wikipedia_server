// Very small wikitext-to-HTML converter for readable previews.
// It handles the common constructs (headings, emphasis, links, lists) and
// strips what it cannot render (templates, tables, references). The raw
// wikitext stays available through the UI toggle.

export interface Heading {
  level: number;
  text: string;
  id: string;
}

export interface RenderedArticle {
  html: string;
  categories: string[];
  headings: Heading[];
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** Remove {{...}} blocks, innermost first, so nesting unwinds. */
function stripTemplates(text: string): string {
  let prev;
  do {
    prev = text;
    text = text.replace(/\{\{[^{}]*\}\}/gs, "");
  } while (text !== prev);
  return text;
}

/** Remove {| ... |} table blocks (possibly nested). */
function stripTables(text: string): string {
  let prev;
  do {
    prev = text;
    text = text.replace(/\{\|(?:(?!\{\|)[\s\S])*?\|\}/g, "");
  } while (text !== prev);
  return text;
}

/** Anchor slug consistent with how heading ids are generated below. */
function slugify(text: string): string {
  return text.trim().replace(/\s+/g, "_");
}

function uniqueSlug(base: string, used: Map<string, number>): string {
  const count = used.get(base) ?? 0;
  used.set(base, count + 1);
  return count === 0 ? base : `${base}_${count + 1}`;
}

function normalizeForCompare(s: string): string {
  return s.replace(/_/g, " ").trim();
}

const categoryRe = /\[\[\s*:?\s*[Cc]ategory\s*:([^\]|]+)(?:\|[^\]]*)?\]\]/g;

/** Strips [[Category:...]] links out of the flowing text, collecting them. */
function extractCategories(line: string, categories: string[]): string {
  return line.replace(categoryRe, (_m, name) => {
    const cat = name.trim();
    if (cat && !categories.includes(cat)) categories.push(cat);
    return "";
  });
}

// Finds the index just past the "]]" that matches the "[[" at openPos,
// tracking nesting depth so a File/Image caption containing its own
// [[link]] doesn't truncate the match early. Returns -1 if unterminated.
function findMatchingClose(text: string, openPos: number): number {
  let depth = 0;
  for (let i = openPos; i < text.length - 1; i++) {
    if (text[i] === "[" && text[i + 1] === "[") {
      depth++;
      i++;
    } else if (text[i] === "]" && text[i + 1] === "]") {
      depth--;
      i++;
      if (depth === 0) return i + 1;
    }
  }
  return -1;
}

/** Splits on sep at bracket-depth 0, so a caption's own [[a|b]] isn't split. */
function splitTopLevel(inner: string, sep: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let current = "";
  for (let i = 0; i < inner.length; i++) {
    if (inner.startsWith("[[", i)) {
      depth++;
      current += "[[";
      i++;
      continue;
    }
    if (inner.startsWith("]]", i)) {
      depth--;
      current += "]]";
      i++;
      continue;
    }
    if (inner[i] === sep && depth === 0) {
      parts.push(current);
      current = "";
      continue;
    }
    current += inner[i];
  }
  parts.push(current);
  return parts;
}

const FILE_OPTION_RE =
  /^(thumb(nail)?|frame(d)?|border|right|left|center|centre|none|baseline|middle|sub|super|top|text-top|bottom|text-bottom|upright(=?\d*\.?\d*)?|\d+x?\d*px|alt=.*|link=.*|page=.*|class=.*|lang=.*)$/i;

/** Picks the caption parameter (if any) out of a File/Image link's body. */
function extractFileCaption(inner: string): string {
  const params = splitTopLevel(inner, "|").slice(1); // drop "File:Foo.jpg"
  for (let i = params.length - 1; i >= 0; i--) {
    const p = params[i].trim();
    if (p && !FILE_OPTION_RE.test(p)) return p;
  }
  return "";
}

// Replaces [[File:...]]/[[Image:...]] blocks with just their caption text
// (if any), since we can't display the image itself. Depth-aware, so a
// caption containing its own [[link]] or ''emphasis'' survives intact and
// is processed normally afterward - a plain [^\]]* regex would instead stop
// at the first "]" inside such a caption and leave stray brackets behind.
function stripFileLinks(text: string): string {
  let out = "";
  let i = 0;
  while (i < text.length) {
    if (text.startsWith("[[", i) && /^(File|Image):/i.test(text.slice(i + 2, i + 30))) {
      const end = findMatchingClose(text, i);
      if (end === -1) {
        out += text.slice(i);
        break;
      }
      const caption = extractFileCaption(text.slice(i + 2, end - 2));
      if (caption) out += `(${caption})`;
      i = end;
    } else {
      out += text[i];
      i++;
    }
  }
  return out;
}

// Namespaces that are legitimately part of the same wiki (as opposed to an
// interwiki prefix like "fr:" or "commons:", which we have nowhere to send
// the reader to). Talk/User pages are outside the "articles" dump this
// server reads, so linking to them can still 404 - that's an accurate
// reflection of the data, not a dead link bug.
const KNOWN_NAMESPACES = new Set([
  "category",
  "template",
  "talk",
  "user",
  "user talk",
  "help",
  "portal",
  "module",
  "draft",
  "mediawiki",
  "special",
  "book",
  "file",
  "image",
]);

interface LinkResolution {
  kind: "page" | "anchor" | "text";
  source?: string;
  title?: string;
  anchor?: string;
}

// Figures out what a [[target]] should do: jump to a heading on the current
// page, link to a page (optionally on the other loaded source, for wikt:/w:
// interwiki prefixes), or - for a genuinely unresolvable interwiki prefix
// like "fr:" or "commons:" - render as plain text instead of a dead link.
function resolveLink(rawTarget: string, currentTitle: string): LinkResolution {
  let target = rawTarget.trim();
  if (target.startsWith(":")) target = target.slice(1).trim();

  let base = target;
  let anchor = "";
  const hashIdx = target.indexOf("#");
  if (hashIdx !== -1) {
    base = target.slice(0, hashIdx).trim();
    anchor = target.slice(hashIdx + 1).trim();
  }

  const isSamePage = base === "" || normalizeForCompare(base) === normalizeForCompare(currentTitle);
  if (anchor && isSamePage) {
    return { kind: "anchor", anchor: slugify(anchor) };
  }
  if (base === "") {
    return { kind: "text" };
  }

  const prefixMatch = base.match(/^([A-Za-z][A-Za-z_ -]*):(.+)$/);
  if (prefixMatch) {
    const prefix = prefixMatch[1].trim().toLowerCase();
    const rest = prefixMatch[2].trim();
    const anchorSlug = anchor ? slugify(anchor) : undefined;
    if (prefix === "w" || prefix === "wikipedia") {
      return { kind: "page", source: "wiki", title: rest, anchor: anchorSlug };
    }
    if (prefix === "wikt" || prefix === "wiktionary") {
      return { kind: "page", source: "dict", title: rest, anchor: anchorSlug };
    }
    if (KNOWN_NAMESPACES.has(prefix)) {
      return { kind: "page", title: base, anchor: anchorSlug };
    }
    return { kind: "text" };
  }

  return { kind: "page", title: base, anchor: anchor ? slugify(anchor) : undefined };
}

// MediaWiki's "pipe trick": [[Target (disambiguation)|]] or
// [[Namespace:Target, extra|]] fills in the label automatically.
function derivePipeTrickLabel(target: string): string {
  let t = target.trim();
  if (t.startsWith(":")) t = t.slice(1);
  const colonIdx = t.indexOf(":");
  if (colonIdx !== -1 && /^[A-Za-z][A-Za-z_ -]*$/.test(t.slice(0, colonIdx))) {
    t = t.slice(colonIdx + 1);
  }
  const paren = t.match(/^(.*\S)\s*\([^()]*\)$/);
  if (paren) return paren[1].trim();
  const comma = t.indexOf(",");
  if (comma !== -1) return t.slice(0, comma).trim();
  return t.trim();
}

function renderLink(rawTarget: string, label: string, currentTitle: string): string {
  const resolved = resolveLink(rawTarget, currentTitle);
  if (resolved.kind === "anchor") {
    return `<a class="anchor-link" data-anchor="${resolved.anchor}">${label}</a>`;
  }
  if (resolved.kind === "page") {
    const attrs = [`data-page="${resolved.title}"`];
    if (resolved.source) attrs.push(`data-source="${resolved.source}"`);
    if (resolved.anchor) attrs.push(`data-anchor="${resolved.anchor}"`);
    return `<a class="ilink" ${attrs.join(" ")}>${label}</a>`;
  }
  return label;
}

function renderLinks(line: string, currentTitle: string): string {
  line = line.replace(/\[\[([^\]|]+)\|([^\]]*)\]\]/g, (_m, target, label) =>
    renderLink(target, label.trim() || derivePipeTrickLabel(target), currentTitle),
  );
  line = line.replace(/\[\[([^\]]+)\]\]/g, (_m, target) => renderLink(target, target.trim(), currentTitle));
  return line;
}

function inlineMarkup(line: string, currentTitle: string): string {
  line = stripFileLinks(line);
  line = line
    // '''''bold italic'''''
    .replace(/'''''(.+?)'''''/g, "<b><i>$1</i></b>")
    .replace(/'''(.+?)'''/g, "<b>$1</b>")
    .replace(/''(.+?)''/g, "<i>$1</i>");
  line = renderLinks(line, currentTitle);
  line = line
    // [http://... label]
    .replace(
      /\[(https?:\/\/[^\s\]]+)\s+([^\]]+)\]/g,
      '<a href="$1" target="_blank" rel="noopener">$2</a>',
    )
    .replace(
      /\[(https?:\/\/[^\s\]]+)\]/g,
      '<a href="$1" target="_blank" rel="noopener">$1</a>',
    );
  return line;
}

// Matches either a known HTML entity (produced by escapeHtml, left alone -
// splitting one up, e.g. wrapping just "amp" inside "&amp;", would corrupt
// it) or a word: a run of letters/digits, allowing an apostrophe inside a
// contraction ("don't") but not a hyphen, so a hyphenated compound like
// "German-born" still yields two independently useful links rather than one
// link to a compound title that almost certainly doesn't exist.
const WORD_OR_ENTITY_RE = /&(?:amp|lt|gt|quot);|[\p{L}\p{N}][\p{L}\p{N}'’]*/gu;

// Makes every remaining plain word clickable to its own page, on top of the
// explicit [[links]] already rendered above. Operates on the finished HTML
// string, alternating between tag chunks (left untouched) and text chunks
// (word-wrapped) via the capturing split below, and tracks <a>/</a> nesting
// so a word that's already part of a link (e.g. the label of a [[target|
// label]] link, or of a same-page anchor link) never gets a second, nested
// <a> wrapped around it.
function linkifyWords(html: string): string {
  let inAnchor = false;
  return html
    .split(/(<[^>]+>)/)
    .map((part) => {
      if (part.startsWith("<")) {
        const tag = part.match(/^<\/?\s*([a-zA-Z][a-zA-Z0-9]*)/)?.[1]?.toLowerCase();
        if (tag === "a") inAnchor = !part.startsWith("</");
        return part;
      }
      if (inAnchor || part === "") return part;
      return part.replace(WORD_OR_ENTITY_RE, (token) =>
        token.startsWith("&") ? token : `<a class="ilink" data-page="${token}">${token}</a>`,
      );
    })
    .join("");
}

export function renderWikitext(source: string, currentTitle: string): RenderedArticle {
  let text = source
    .replace(/<!--[\s\S]*?-->/g, "")
    // The (?=[\s/>]) lookahead pins down where the tag name actually ends -
    // without it, "<ref[^>]*>" also matches the unrelated "<references>"
    // (since "ref" is a text-prefix of "references"), silently swallowing
    // its opening tag and leaving a stray, unmatched "</references>" behind
    // (its own closing-tag regex needs a literal ">" right after "ref", so
    // it never had this problem).
    .replace(/<ref(?=[\s/>])[^>/]*\/>/gi, "")
    .replace(/<ref(?=[\s/>])[^>]*>[\s\S]*?<\/ref>/gi, "")
    // <references>...</references> (or self-closing) is where the footnote
    // list defined by the <ref> tags above would render; since we drop
    // <ref> contents entirely, it has nothing left to show.
    .replace(/<references(?=[\s/>])[^>]*\/>/gi, "")
    .replace(/<references(?=[\s/>])[^>]*>[\s\S]*?<\/references>/gi, "")
    .replace(/<(gallery|math|syntaxhighlight|source|poem|score|timeline)[^>]*>[\s\S]*?<\/\1>/gi, "");
  text = stripTemplates(text);
  text = stripTables(text);
  text = escapeHtml(text);

  const categories: string[] = [];
  const headings: Heading[] = [];
  const usedSlugs = new Map<string, number>();
  const out: string[] = [];
  let paragraph: string[] = [];
  let listTag: "ul" | "ol" | null = null;

  const closeList = () => {
    if (listTag) {
      out.push(`</${listTag}>`);
      listTag = null;
    }
  };
  const flushParagraph = () => {
    const joined = paragraph.join(" ").trim();
    if (joined) out.push(`<p>${joined}</p>`);
    paragraph = [];
  };

  for (const sourceLine of text.split("\n")) {
    const rawLine = extractCategories(sourceLine, categories);
    const line = rawLine.trim();

    const heading = line.match(/^(={2,6})\s*(.+?)\s*\1$/);
    if (heading) {
      flushParagraph();
      closeList();
      const level = Math.min(heading[1].length, 6);
      const headingText = heading[2];
      const id = uniqueSlug(slugify(headingText), usedSlugs);
      headings.push({ level, text: headingText, id });
      out.push(`<h${level} id="${id}">${inlineMarkup(headingText, currentTitle)}</h${level}>`);
      continue;
    }

    // (.*) rather than (.+): a bullet whose entire content was a citation
    // template (extremely common in references/further-reading sections)
    // becomes a bare marker once stripTemplates removes it. Matching (and
    // then discarding) that case here keeps it from falling through to the
    // plain-paragraph branch below, where runs of such bare markers used to
    // get joined into a stray line of literal asterisks.
    const list = line.match(/^([*#]+)\s*(.*)$/);
    if (list) {
      const content = inlineMarkup(list[2], currentTitle).trim();
      if (content) {
        flushParagraph();
        const tag = list[1][0] === "#" ? "ol" : "ul";
        if (listTag !== tag) {
          closeList();
          out.push(`<${tag}>`);
          listTag = tag;
        }
        out.push(`<li>${content}</li>`);
      }
      continue;
    }

    if (line === "") {
      flushParagraph();
      closeList();
      continue;
    }

    closeList();
    paragraph.push(inlineMarkup(line, currentTitle));
  }
  flushParagraph();
  closeList();

  return { html: linkifyWords(out.join("\n")), categories, headings };
}
