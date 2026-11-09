import type { SyntaxToken } from './highlight';

const COLUMN_SEPARATOR = '  ';

/**
 * Highlights a single line of prettified (column-aligned) CSV.
 *
 * Each column occupies exactly `widths[i]` characters followed by a two-space
 * separator, so columns can be coloured alternately using only the shared
 * widths and the current line. When no widths are available (prettify off) the
 * line is returned as a single plain token.
 */
export function highlightCsvLine(line: string, widths: number[]): SyntaxToken[] {
  if (widths.length === 0) {
    return [{ text: line, type: 'plain' }];
  }

  const tokens: SyntaxToken[] = [];
  let pos = 0;

  for (let col = 0; col < widths.length && pos < line.length; col++) {
    const end = Math.min(line.length, pos + widths[col]);
    tokens.push({
      text: line.slice(pos, end),
      type: col % 2 === 0 ? 'column-even' : 'column-odd',
    });
    pos = end;

    if (col < widths.length - 1 && pos < line.length) {
      const sepEnd = Math.min(line.length, pos + COLUMN_SEPARATOR.length);
      tokens.push({ text: line.slice(pos, sepEnd), type: 'plain' });
      pos = sepEnd;
    }
  }

  if (pos < line.length) {
    tokens.push({ text: line.slice(pos), type: 'plain' });
  }

  return tokens;
}
