import { useCallback, useEffect, useRef, useState } from "react";
import { Article, fetchPage, fetchRandom, fetchSources, Source } from "./api";
import SearchBar from "./components/SearchBar";
import ArticleView from "./components/ArticleView";

interface Location {
  source: string;
  page: string;
  anchor: string;
}

function readLocation(): Location {
  const params = new URLSearchParams(window.location.search);
  return {
    source: params.get("source") ?? "wiki",
    page: params.get("page") ?? "",
    anchor: decodeURIComponent(window.location.hash.replace(/^#/, "")),
  };
}

export default function App() {
  const [sources, setSources] = useState<Source[]>([]);
  const [{ source, page, anchor }, setLocation] = useState(readLocation);
  const [article, setArticle] = useState<Article | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    fetchSources().then(setSources).catch(() => setSources([]));
  }, []);

  useEffect(() => {
    const onPop = () => setLocation(readLocation());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  // "/" focuses search from anywhere, like most wiki/docs sites.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement;
      if (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable) return;
      e.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    document.title = page ? `${page} — Wikipedia Server` : "Wikipedia Server";
  }, [page]);

  const navigate = useCallback((nextSource: string, nextPage: string, nextAnchor?: string) => {
    const params = new URLSearchParams();
    params.set("source", nextSource);
    if (nextPage) params.set("page", nextPage);
    const hash = nextAnchor ? "#" + encodeURIComponent(nextAnchor) : "";
    window.history.pushState(null, "", "?" + params.toString() + hash);
    setLocation({ source: nextSource, page: nextPage, anchor: nextAnchor ?? "" });
  }, []);

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

  const goRandom = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchRandom(source)
      .then((a) => {
        navigate(source, a.title);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [source, navigate]);

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
        <SearchBar
          ref={searchRef}
          source={source}
          onSelect={(title) => navigate(source, title)}
        />
        <button className="random-btn" title="Random article (or press /)" onClick={goRandom}>
          <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden>
            <path
              fill="currentColor"
              d="M17 3h4v4h-2V6.4l-4.6 4.6-1.4-1.4L17.6 5H17V3zM3 6a1 1 0 011-1h5v2H5.4l4.3 4.3-1.4 1.4L4 8.4V13H2V7a1 1 0 011-1zm0 12a1 1 0 001 1h5v-2H5.4l4.3-4.3-1.4-1.4L4 15.6V11H2v6a1 1 0 001 1zm18-1v-5h-2v3.6l-4.3-4.3-1.4 1.4 4.3 4.3H16v2h6a1 1 0 001-1z"
            />
          </svg>
          Random
        </button>
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
              or <Suggestion onGo={(t) => navigate(source, t)} title="Albert Einstein" />, press{" "}
              <code>/</code> to search, or hit <code>Random</code>. The JSON API lives at{" "}
              <code>/api/{source}/page/&lt;title&gt;</code> (docs at{" "}
              <a href="/swagger/">/swagger/</a>).
            </p>
          </div>
        )}
        {article && !loading && (
          <ArticleView
            article={article}
            scrollToAnchor={anchor || undefined}
            onNavigate={(title, opts) => navigate(opts?.source ?? source, title, opts?.anchor)}
          />
        )}
      </main>

      <footer className="footer">
        <a href="/api/status">API status</a>
        <span>·</span>
        <a href="/swagger/">API docs</a>
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
