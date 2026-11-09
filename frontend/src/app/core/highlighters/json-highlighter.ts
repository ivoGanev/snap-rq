import type { SyntaxToken } from './highlight';

const PUNCTUATION = new Set(['{', '}', '[', ']', ',', ':']);

const NUMBER_RE = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/;
const LITERAL_RE = /^(true|false|null)(?![a-zA-Z0-9_])/;

/**
 * Single-pass, per-line JSON highlighter.
 *
 * Because pretty-printed JSON never breaks a token across a line break, a line
 * in isolation contains complete, highlightable tokens. Each call returns a
 * contiguous token stream covering the whole line.
 */
export function highlightJsonLine(line: string): SyntaxToken[] {
  const tokens: SyntaxToken[] = [];
  let i = 0;
  let plainStart = 0;

  const flushPlain = (upto: number): void => {
    if (upto > plainStart) {
      tokens.push({ text: line.slice(plainStart, upto), type: 'plain' });
    }
  };

  while (i < line.length) {
    const ch = line[i];

    if (ch === '"') {
      let j = i + 1;
      let closed = false;
      while (j < line.length) {
        const c = line[j];
        if (c === '\\') {
          j += 2;
        } else if (c === '"') {
          closed = true;
          j++;
          break;
        } else {
          j++;
        }
      }
      const end = closed ? j : line.length;

      // A string immediately followed (ignoring whitespace) by `:` is a key.
      let k = end;
      while (k < line.length && (line[k] === ' ' || line[k] === '\t')) {
        k++;
      }
      const isKey = line[k] === ':';

      flushPlain(i);
      tokens.push({ text: line.slice(i, end), type: isKey ? 'key' : 'string' });
      plainStart = end;
      i = end;
      continue;
    }

    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      const match = NUMBER_RE.exec(line.slice(i));
      if (match) {
        flushPlain(i);
        tokens.push({ text: match[0], type: 'number' });
        i += match[0].length;
        plainStart = i;
        continue;
      }
    }

    if (ch === 't' || ch === 'f' || ch === 'n') {
      const match = LITERAL_RE.exec(line.slice(i));
      if (match) {
        flushPlain(i);
        tokens.push({
          text: match[0],
          type: match[0] === 'null' ? 'null' : 'boolean',
        });
        i += match[0].length;
        plainStart = i;
        continue;
      }
    }

    if (PUNCTUATION.has(ch)) {
      flushPlain(i);
      tokens.push({ text: ch, type: 'punctuation' });
      i++;
      plainStart = i;
      continue;
    }

    i++;
  }

  flushPlain(line.length);
  return tokens;
}
