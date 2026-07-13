// Very small wikitext-to-HTML converter for readable previews.
// It handles the common constructs (headings, emphasis, links, lists) and
// strips what it cannot render (templates, tables, references). The raw
// wikitext stays available through the UI toggle.

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

function inlineMarkup(line: string): string {
  return (
    line
      // '''''bold italic'''''
      .replace(/'''''(.+?)'''''/g, "<b><i>$1</i></b>")
      .replace(/'''(.+?)'''/g, "<b>$1</b>")
      .replace(/''(.+?)''/g, "<i>$1</i>")
      // [[File:...]] and [[Image:...]] carry no useful text here.
      .replace(/\[\[(?:File|Image):[^\]]*\]\]/gi, "")
      // [[target|label]] and [[target]]
      .replace(
        /\[\[([^\]|]+)\|([^\]]+)\]\]/g,
        (_m, target, label) =>
          `<a class="ilink" data-page="${target.trim()}">${label}</a>`,
      )
      .replace(
        /\[\[([^\]]+)\]\]/g,
        (_m, target) =>
          `<a class="ilink" data-page="${target.trim()}">${target}</a>`,
      )
      // [http://... label]
      .replace(
        /\[(https?:\/\/[^\s\]]+)\s+([^\]]+)\]/g,
        '<a href="$1" target="_blank" rel="noopener">$2</a>',
      )
      .replace(
        /\[(https?:\/\/[^\s\]]+)\]/g,
        '<a href="$1" target="_blank" rel="noopener">$1</a>',
      )
  );
}

export function renderWikitext(source: string): string {
  let text = source
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<ref[^>/]*\/>/gi, "")
    .replace(/<ref[^>]*>[\s\S]*?<\/ref>/gi, "");
  text = stripTemplates(text);
  text = stripTables(text);
  text = escapeHtml(text);

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

  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();

    const heading = line.match(/^(={2,6})\s*(.+?)\s*\1$/);
    if (heading) {
      flushParagraph();
      closeList();
      const level = Math.min(heading[1].length, 6);
      out.push(`<h${level}>${inlineMarkup(heading[2])}</h${level}>`);
      continue;
    }

    const list = line.match(/^([*#]+)\s*(.+)$/);
    if (list) {
      flushParagraph();
      const tag = list[1][0] === "#" ? "ol" : "ul";
      if (listTag !== tag) {
        closeList();
        out.push(`<${tag}>`);
        listTag = tag;
      }
      out.push(`<li>${inlineMarkup(list[2])}</li>`);
      continue;
    }

    if (line === "") {
      flushParagraph();
      closeList();
      continue;
    }

    closeList();
    paragraph.push(inlineMarkup(line));
  }
  flushParagraph();
  closeList();

  return out.join("\n");
}
