export interface Source {
  name: string;
  description: string;
  pages: number;
}

export interface SearchResult {
  title: string;
  id: number;
}

export interface SearchResponse {
  source: string;
  query: string;
  results: SearchResult[];
}

export interface Article {
  source: string;
  title: string;
  id: number;
  ns: number;
  revisionId?: string;
  timestamp?: string;
  redirectTo?: string;
  redirectedFrom?: string[];
  text: string;
}

async function getJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url);
  const body = await resp.json().catch(() => null);
  if (!resp.ok) {
    const message = body?.error?.message ?? `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return body as T;
}

export function fetchSources(): Promise<Source[]> {
  return getJSON("/api/sources");
}

export function searchTitles(
  source: string,
  query: string,
  limit = 10,
): Promise<SearchResponse> {
  const params = new URLSearchParams({ q: query, limit: String(limit) });
  return getJSON(`/api/${source}/search?${params}`);
}

export function fetchPage(source: string, title: string): Promise<Article> {
  return getJSON(`/api/${source}/page/${encodeURIComponent(title)}`);
}

export function fetchRandom(source: string): Promise<Article> {
  return getJSON(`/api/${source}/random`);
}
