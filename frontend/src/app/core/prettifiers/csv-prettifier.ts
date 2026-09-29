import { Prettifier } from './prettifier';

const MAX_COLUMN_WIDTH = 80;

/**
 * Parses a CSV string into rows, respecting double-quote escaping.
 *
 * This is intentionally simple: it supports quoted fields and escaped quotes
 * ("") but does not aim to cover every CSV edge case. It is sufficient for
 * formatting typical API CSV responses.
 */
function parseCsv(input: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let insideQuotes = false;

  for (let i = 0; i < input.length; i++) {
    const char = input[i];
    const next = input[i + 1];

    if (insideQuotes) {
      if (char === '"' && next === '"') {
        cell += '"';
        i++;
      } else if (char === '"') {
        insideQuotes = false;
      } else {
        cell += char;
      }
    } else {
      if (char === '"') {
        insideQuotes = true;
      } else if (char === ',') {
        row.push(cell);
        cell = '';
      } else if (char === '\n') {
        row.push(cell);
        if (row.length > 0 && row.some(c => c.length > 0)) {
          rows.push(row);
        }
        row = [];
        cell = '';
      } else if (char === '\r') {
        // Skip carriage returns; rely on \n for line breaks.
      } else {
        cell += char;
      }
    }
  }

  if (cell.length > 0 || row.length > 0) {
    row.push(cell);
    rows.push(row);
  }

  return rows;
}

function isBlankRow(row: string[]): boolean {
  return row.every(cell => cell.length === 0);
}

/**
 * Prettifier for CSV response bodies.
 *
 * Detects CSV either by Content-Type or by checking that the body looks like
 * comma-separated rows, then reformats the data into aligned columns.
 */
export class CsvPrettifier extends Prettifier {
  readonly name = 'csv';

  canPrettify(raw: string, contentType?: string): boolean {
    if (contentType?.includes('csv')) {
      return true;
    }

    const trimmed = raw.trim();
    if (trimmed.length === 0) {
      return false;
    }

    // Avoid treating JSON as CSV even though JSON contains commas.
    if (/^\s*[\[{]/.test(trimmed)) {
      return false;
    }

    const lines = trimmed.split(/\r?\n/).filter(line => line.length > 0);
    return lines.length >= 2 && lines.slice(0, 2).every(line => line.includes(','));
  }

  prettify(raw: string): string {
    const rows = parseCsv(raw).filter(row => !isBlankRow(row));
    if (rows.length === 0) {
      return raw;
    }

    const columnCount = Math.max(...rows.map(row => row.length));
    const widths: number[] = Array.from({ length: columnCount }, () => 0);

    for (const row of rows) {
      for (let i = 0; i < columnCount; i++) {
        const cell = row[i] ?? '';
        widths[i] = Math.min(MAX_COLUMN_WIDTH, Math.max(widths[i], cell.length));
      }
    }

    return rows
      .map(row =>
        Array.from({ length: columnCount }, (_, i) => {
          const cell = row[i] ?? '';
          const width = widths[i] ?? 0;
          return cell.length > width ? cell : cell.padEnd(width, ' ');
        }).join('  '),
      )
      .join('\n');
  }
}
