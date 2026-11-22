import type { HttpRequestSummary, RequestUsage } from './models/http';

/**
 * Request search: every word of the query must match (in any order), in a
 * request's name, URL, method or collection name. Results are ranked by how
 * well the weakest word matches, then by recent usage, then by name.
 *
 * The index is built once per list and reused for every keystroke.
 */

/** A request with its searchable text prepared. */
export interface SearchEntry {
  req: HttpRequestSummary;
  name: string;
  /** The name's words, split on punctuation and camelCase ("getUserList" → get, user, list). */
  nameWords: string[];
  /** The name without separators, for loose (fuzzy) matches. */
  nameLoose: string;
  url: string;
  /** The URL's path parts, after the scheme and host ("/users/{{id}}" → users, id). */
  pathParts: string[];
  method: string;
  collection: string;
}

/** How well a word matched: lower is better. */
enum Tier {
  /** The name starts with it, or a word in the name does. */
  NameStart = 0,
  /** The name contains it, or a part of the URL path starts with it. */
  Good = 1,
  /** Anywhere else: URL, method, collection name, or loosely in the name. */
  Weak = 2,
}

export function buildSearchIndex(
  requests: HttpRequestSummary[],
  collectionName: (collectionId: number) => string | undefined,
): SearchEntry[] {
  return requests.map((req) => {
    const name = req.name.toLowerCase();
    const url = req.url.toLowerCase();
    const path = url.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]*/, '');
    return {
      req,
      name,
      nameWords: req.name
        .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
        .toLowerCase()
        .split(/[^a-z0-9]+/)
        .filter(Boolean),
      nameLoose: name.replace(/[^a-z0-9]+/g, ''),
      url,
      pathParts: path.split(/[^a-z0-9_-]+/).filter(Boolean),
      method: req.method.toLowerCase(),
      collection: (collectionName(req.collection_id) ?? '').toLowerCase(),
    };
  });
}

/** The query's words, lower-cased (empty for a blank query). */
export function queryWords(query: string): string[] {
  return query.toLowerCase().split(/\s+/).filter(Boolean);
}

/** The best tier a word reaches in a request, or null when it doesn't match. */
function wordTier(entry: SearchEntry, word: string): Tier | null {
  if (entry.name.startsWith(word) || entry.nameWords.some((w) => w.startsWith(word))) {
    return Tier.NameStart;
  }
  if (entry.name.includes(word) || entry.pathParts.some((p) => p.startsWith(word))) {
    return Tier.Good;
  }
  if (
    entry.url.includes(word) ||
    entry.method.startsWith(word) ||
    entry.collection.includes(word) ||
    (word.length >= 3 && isSubsequence(word, entry.nameLoose))
  ) {
    return Tier.Weak;
  }
  return null;
}

/** Whether every letter of word appears in text, in order ("usrlst" in "getuserlist"). */
function isSubsequence(word: string, text: string): boolean {
  let i = 0;
  for (const char of text) {
    if (char === word[i]) i++;
    if (i === word.length) return true;
  }
  return false;
}

/** A request's tier for all words (its weakest word's), or null if any word misses. */
function entryTier(entry: SearchEntry, words: string[]): Tier | null {
  let worst = Tier.NameStart;
  for (const word of words) {
    const tier = wordTier(entry, word);
    if (tier === null) return null;
    if (tier > worst) worst = tier;
  }
  return worst;
}

/** The requests matching every word, in the index's order. */
export function filterRequests(index: SearchEntry[], words: string[]): HttpRequestSummary[] {
  return index.filter((e) => entryTier(e, words) !== null).map((e) => e.req);
}

/** The requests matching every word, best first: match, then recent usage, then name. */
export function rankRequests(
  index: SearchEntry[],
  words: string[],
  usage: Record<number, RequestUsage>,
): HttpRequestSummary[] {
  const hits: { req: HttpRequestSummary; tier: Tier; score: number }[] = [];
  for (const entry of index) {
    const tier = entryTier(entry, words);
    if (tier !== null) hits.push({ req: entry.req, tier, score: usage[entry.req.id]?.score ?? 0 });
  }
  hits.sort((a, b) => a.tier - b.tier || b.score - a.score || a.req.name.localeCompare(b.req.name));
  return hits.map((h) => h.req);
}
