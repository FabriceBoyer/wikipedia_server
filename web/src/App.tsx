import { useCallback, useEffect, useState } from "react";
import { Article, fetchPage, fetchSources, Source } from "./api";
import SearchBar from "./components/SearchBar";
import ArticleView from "./components/ArticleView";

function readLocation(): { source: string; page: string } {
  const params = new URLSearchParams(window.location.search);
  return { source: params.get("source") ?? "wiki", page: params.get("page") ?? "" };
}

export default function App() {
  const [sources, setSources] = useState<Source[]>([]);
  const [{ source, page }, setLocation] = useState(readLocation);
  const [article, setArticle] = useState<Article | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    fetchSources().then(setSources).catch(() => setSources([]));
  }, []);

  useEffect(() => {
    const onPop = () => setLocation(readLocation());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const navigate = useCallback(
    (nextSource: string, nextPage: string) => {
      const params = new URLSearchParams();
      params.set("source", nextSource);
      if (nextPage) params.set("page", nextPage);
      window.history.pushState(null, "", "?" + params.toString());
      setLocation({ source: nextSource, page: nextPage });
    },
    [],
  );

  useEffect(() => {
    if (!page) {
      setArticle(null);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchPage(source, page)
      .then((a) => {
        if (!cancelled) setArticle(a);
      })
      .catch((e: Error) => {
        if (!cancelled) {
          setArticle(null);
          setError(e.message);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [source, page]);

  const currentSource = sources.find((s) => s.name === source);

  return (
    <div className="app">
      <header className="topbar">
        <a
          className="brand"
          href="?"
          onClick={(e) => {
            e.preventDefault();
            navigate(source, "");
          }}
        >
          <span className="brand-mark">W</span>
          <span>ikipedia&nbsp;Server</span>
        </a>
        <nav className="tabs" aria-label="Sources">
          {sources.map((s) => (
            <button
              key={s.name}
              className={"tab" + (s.name === source ? " active" : "")}
              title={`${s.description} — ${s.pages.toLocaleString()} pages`}
              onClick={() => navigate(s.name, page)}
            >
              {s.name === "wiki" ? "Wikipedia" : s.name === "dict" ? "Wiktionary" : s.name}
            </button>
          ))}
        </nav>
        <SearchBar source={source} onSelect={(title) => navigate(source, title)} />
      </header>

      <main>
        {loading && <div className="notice">Loading…</div>}
        {error && <div className="notice error">{error}</div>}
        {!page && !loading && (
          <div className="hero">
            <h1>
              Browse Wikipedia dumps, <em>offline</em>.
            </h1>
            <p>
              {currentSource
                ? `${currentSource.description} — ${currentSource.pages.toLocaleString()} pages served straight from the local dump.`
                : "Articles are served straight from local multistream dumps."}
            </p>
            <p className="hint">
              Try searching for <Suggestion onGo={(t) => navigate(source, t)} title="Anarchism" />{" "}
              or <Suggestion onGo={(t) => navigate(source, t)} title="Albert Einstein" />, or use
              the JSON API directly: <code>/api/{source}/page/&lt;title&gt;</code>
            </p>
          </div>
        )}
        {article && !loading && (
          <ArticleView article={article} onNavigate={(title) => navigate(source, title)} />
        )}
      </main>

      <footer className="footer">
        <a href="/api/status">API status</a>
        <span>·</span>
        <a href="https://github.com/fabriceboyer/wikipedia_server" rel="noopener">
          Source
        </a>
      </footer>
    </div>
  );
}

function Suggestion({ title, onGo }: { title: string; onGo: (t: string) => void }) {
  return (
    <a
      className="ilink"
      href={"?page=" + encodeURIComponent(title)}
      onClick={(e) => {
        e.preventDefault();
        onGo(title);
      }}
    >
      {title}
    </a>
  );
}
