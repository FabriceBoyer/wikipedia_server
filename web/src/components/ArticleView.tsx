import { useMemo, useState } from "react";
import { Article } from "../api";
import { renderWikitext } from "../wikitext";

interface Props {
  article: Article;
  onNavigate: (title: string) => void;
}

export default function ArticleView({ article, onNavigate }: Props) {
  const [showRaw, setShowRaw] = useState(false);
  const html = useMemo(() => renderWikitext(article.text), [article.text]);

  const onClick = (e: React.MouseEvent) => {
    const target = (e.target as HTMLElement).closest("a.ilink");
    if (target) {
      e.preventDefault();
      const page = target.getAttribute("data-page");
      if (page) onNavigate(page);
    }
  };

  return (
    <article className="article">
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
          <div
            className="content"
            onClick={onClick}
            dangerouslySetInnerHTML={{ __html: html }}
          />
          <p className="disclaimer">
            Simplified preview — templates, tables and references are omitted.
            Switch to wikitext for the full source.
          </p>
        </>
      )}
    </article>
  );
}
