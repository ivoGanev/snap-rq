import { Prettifier } from './prettifier';
import { JsonPrettifier } from './json-prettifier';
import { CsvPrettifier } from './csv-prettifier';

export { Prettifier, JsonPrettifier, CsvPrettifier };

const PRETTIFIERS: Prettifier[] = [new JsonPrettifier(), new CsvPrettifier()];

/**
 * Extracts the content type from a response headers string.
 *
 * Headers may be stored as plain text lines ("Content-Type: application/json")
 * or as a JSON object. The returned value is lowercased and stripped of any
 * trailing charset/parameters.
 */
function extractContentType(headers: string): string | undefined {
  if (!headers || headers.trim().length === 0) {
    return undefined;
  }

  // Try JSON object form first.
  if (headers.trimStart().startsWith('{')) {
    try {
      const parsed = JSON.parse(headers) as Record<string, string | string[]>;
      for (const [key, value] of Object.entries(parsed)) {
        if (key.toLowerCase() === 'content-type') {
          const raw = Array.isArray(value) ? value[0] : value;
          return normalizeContentType(raw);
        }
      }
      return undefined;
    } catch {
      // Fall through to plain-text parsing.
    }
  }

  for (const line of headers.split('\n')) {
    const separatorIndex = line.indexOf(':');
    if (separatorIndex === -1) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    if (key.toLowerCase() === 'content-type') {
      return normalizeContentType(line.slice(separatorIndex + 1));
    }
  }

  return undefined;
}

function normalizeContentType(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }

  const withoutParameters = value.split(';')[0];
  return withoutParameters.trim().toLowerCase();
}

/**
 * Returns a pretty-printed version of a response body when a matching
 * prettifier is found and prettifying is enabled. Otherwise the raw body is
 * returned unchanged.
 */
export function prettifyResponseBody(
  raw: string,
  headers: string,
  enabled: boolean,
): string {
  if (!enabled || !raw || raw.length === 0) {
    return raw;
  }

  const contentType = extractContentType(headers);

  for (const prettifier of PRETTIFIERS) {
    if (prettifier.canPrettify(raw, contentType)) {
      return prettifier.prettify(raw);
    }
  }

  return raw;
}
