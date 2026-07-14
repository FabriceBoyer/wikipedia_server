import { useEffect, useRef, useState } from "react";
import { Backlink, fetchBacklinks } from "../api";

interface Props {
  source: string;
  title: string;
  onNavigate: (title: string) => void;
}

const RETRY_DELAY_MS = 15000;

// Collapsed by default and fetched lazily on expand: most readers won't
// care, and the underlying index is itself only built in the background
// (a full-dump text scan), so this must always cope with "not ready yet"
// gracefully rather than assume it's there.
export default function WhatLinksHere({ source, title, onNavigate }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notReady, setNotReady] = useState(false);
  const [results, setResults] = useState<Backlink[]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [after, setAfter] = useState<string | undefined>();
  const retryTimer = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    setExpanded(false);
    setLoading(false);
    setError(null);
    setNotReady(false);
    setResults([]);
    setTotal(null);
    setAfter(undefined);
    clearTimeout(retryTimer.current);
  }, [source, title]);

  useEffect(() => () => clearTimeout(retryTimer.current), []);

  const load = (cursor?: string) => {
    setLoading(true);
    setError(null);
    fetchBacklinks(source, title, cursor, 30)
      .then((page) => {
        if (!page.ready) {
          setNotReady(true);
          retryTimer.current = setTimeout(() => load(cursor), RETRY_DELAY_MS);
          return;
        }
        setNotReady(false);
        setTotal(page.total ?? 0);
        setResults((prev) => (cursor ? [...prev, ...(page.results ?? [])] : (page.results ?? [])));
        setAfter(page.nextAfter);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  };

  const toggle = () => {
    setExpanded((was) => {
      const next = !was;
      if (next && total === null && !loading) load();
      return next;
    });
  };

  return (
    <section className="whatlinkshere">
      <button className="whatlinkshere-toggle" onClick={toggle} aria-expanded={expanded}>
        <span className={"chevron" + (expanded ? " open" : "")}>▸</span>
        What links here{total !== null ? ` (${total.toLocaleString()})` : ""}
      </button>
      {expanded && (
        <div className="whatlinkshere-body">
          {loading && results.length === 0 && <p className="notice-inline">Loading…</p>}
          {notReady && (
            <p className="notice-inline">
              Still indexing incoming links across the dump — this can take a
              while on large dumps. Checking again periodically…
            </p>
          )}
          {error && <p className="notice-inline error">{error}</p>}
          {total === 0 && <p className="notice-inline">No pages link here.</p>}
          {results.length > 0 && (
            <ul className="whatlinkshere-list">
              {results.map((b) => (
                <li key={b.title}>
                  <a
                    className="ilink"
                    href={`?source=${source}&page=${encodeURIComponent(b.title)}`}
                    onClick={(e) => {
                      e.preventDefault();
                      onNavigate(b.title);
                    }}
                  >
                    {b.title}
                  </a>
                </li>
              ))}
            </ul>
          )}
          {after && (
            <button className="chip toggle" disabled={loading} onClick={() => load(after)}>
              {loading ? "Loading…" : "Show more"}
            </button>
          )}
        </div>
      )}
    </section>
  );
}
