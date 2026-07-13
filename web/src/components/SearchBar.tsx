import { forwardRef, useEffect, useRef, useState } from "react";
import { searchTitles, SearchResult } from "../api";

interface Props {
  source: string;
  onSelect: (title: string) => void;
}

const SearchBar = forwardRef<HTMLInputElement, Props>(function SearchBar(
  { source, onSelect },
  inputRef,
) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const boxRef = useRef<HTMLDivElement>(null);

  // Debounced autocompletion.
  useEffect(() => {
    if (query.trim() === "") {
      setResults([]);
      setOpen(false);
      return;
    }
    const timer = setTimeout(() => {
      searchTitles(source, query)
        .then((r) => {
          setResults(r.results);
          setOpen(r.results.length > 0);
          setActive(-1);
        })
        .catch(() => setResults([]));
    }, 150);
    return () => clearTimeout(timer);
  }, [query, source]);

  useEffect(() => {
    const close = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, []);

  const choose = (title: string) => {
    setQuery("");
    setOpen(false);
    onSelect(title);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown" && open) {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, results.length - 1));
    } else if (e.key === "ArrowUp" && open) {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, -1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (active >= 0 && results[active]) choose(results[active].title);
      else if (query.trim() !== "") choose(query.trim());
    } else if (e.key === "Escape") {
      setOpen(false);
    }
  };

  return (
    <div className="searchbox" ref={boxRef}>
      <svg className="searchicon" viewBox="0 0 24 24" width="18" height="18" aria-hidden>
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 1 1-13 0 6.5 6.5 0 0 1 13 0z"
        />
      </svg>
      <input
        ref={inputRef}
        type="search"
        placeholder="Search articles… (press /)"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={onKeyDown}
        onFocus={() => results.length > 0 && setOpen(true)}
        aria-label="Search articles"
        autoComplete="off"
      />
      {open && (
        <ul className="suggestions" role="listbox">
          {results.map((r, i) => (
            <li
              key={r.id}
              role="option"
              aria-selected={i === active}
              className={i === active ? "active" : ""}
              onMouseDown={() => choose(r.title)}
              onMouseEnter={() => setActive(i)}
            >
              {r.title}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
});

export default SearchBar;
