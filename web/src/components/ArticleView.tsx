import { useEffect, useMemo, useRef, useState } from "react";
import { Article } from "../api";
import { renderWikitext } from "../wikitext";

interface Props {
  article: Article;
  scrollToAnchor?: string;
  onNavigate: (title: string, opts?: { source?: string; anchor?: string }) => void;
}

export default function ArticleView({ article, scrollToAnchor, onNavigate }: Props) {
  const [showRaw, setShowRaw] = useState(false);
  const [copied, setCopied] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const { html, categories, headings } = useMemo(
    () => renderWikitext(article.text, article.title),
    [article.text, article.title],
  );

  useEffect(() => {
    setShowRaw(false);
  }, [article.title]);

  useEffect(() => {
    if (showRaw) return;
    const id = scrollToAnchor;
    if (!id) return;
    // Wait a tick for the freshly-rendered HTML to be in the DOM.
    const raf = requestAnimationFrame(() => {
      containerRef.current?.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView({ block: "start" });
    });
    return () => cancelAnimationFrame(raf);
  }, [html, scrollToAnchor, showRaw]);

  const onClick = (e: React.MouseEvent) => {
    const anchorEl = (e.target as HTMLElement).closest("a.anchor-link");
    if (anchorEl) {
      e.preventDefault();
      const id = anchorEl.getAttribute("data-anchor");
      if (id) containerRef.current?.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView({ block: "start" });
      return;
    }
    const linkEl = (e.target as HTMLElement).closest("a.ilink");
    if (linkEl) {
      e.preventDefault();
      const page = linkEl.getAttribute("data-page");
      if (!page) return;
      const source = linkEl.getAttribute("data-source") ?? undefined;
      const anchor = linkEl.getAttribute("data-anchor") ?? undefined;
      onNavigate(page, { source, anchor });
    }
  };

  const copyLink = () => {
    navigator.clipboard?.writeText(window.location.href).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <article className="article" ref={containerRef}>
      <header>
        <h1>{article.title}</h1>
        <div className="meta">
          {article.redirectedFrom && article.redirectedFrom.length > 0 && (
            <span className="chip redirect">
              redirected from {article.redirectedFrom.join(" → ")}
            </span>
          )}
          <span className="chip">id {article.id}</span>
          {article.timestamp && (
            <span className="chip">
              revised {new Date(article.timestamp).toLocaleDateString()}
            </span>
          )}
          <button className="chip toggle" onClick={copyLink}>
            {copied ? "link copied" : "copy link"}
          </button>
          <button
            className={"chip toggle" + (showRaw ? " on" : "")}
            onClick={() => setShowRaw((v) => !v)}
          >
            {showRaw ? "rendered view" : "wikitext"}
          </button>
        </div>
      </header>
      {showRaw ? (
        <pre className="raw">{article.text}</pre>
      ) : (
        <>
          {headings.length >= 4 && (
            <nav className="toc" aria-label="Table of contents">
              <div className="toc-title">Contents</div>
              <ol>
                {headings.map((h) => (
                  <li key={h.id} style={{ marginLeft: (h.level - 2) * 14 }}>
                    <a
                      className="anchor-link"
                      href={`#${h.id}`}
                      onClick={(e) => {
                        e.preventDefault();
                        containerRef.current?.querySelector(`#${CSS.escape(h.id)}`)?.scrollIntoView({ block: "start" });
                      }}
                    >
                      {h.text}
                    </a>
                  </li>
                ))}
              </ol>
            </nav>
          )}
          <div className="content" onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} />
          {categories.length > 0 && (
            <div className="categories">
              <span className="categories-label">Categories:</span>
              {categories.map((c) => (
                <button key={c} className="chip category" onClick={() => onNavigate(`Category:${c}`)}>
                  {c}
                </button>
              ))}
            </div>
          )}
          <p className="disclaimer">
            Simplified preview — templates, tables and references are omitted.
            Switch to wikitext for the full source.
          </p>
        </>
      )}
    </article>
  );
}
