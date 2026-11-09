import { highlightJsonLine } from './json-highlighter';
import { highlightXmlLine } from './xml-highlighter';
import { highlightCsvLine } from './csv-highlighter';

/**
 * Languages the response body highlighter understands. `text` falls back to a
 * single plain token, leaving the body uncoloured.
 */
export type SyntaxLanguage = 'json' | 'xml' | 'csv' | 'text';

export type SyntaxTokenType =
  | 'key'
  | 'string'
  | 'number'
  | 'boolean'
  | 'null'
  | 'punctuation'
  | 'tag'
  | 'attribute'
  | 'comment'
  | 'column-even'
  | 'column-odd'
  | 'plain';

export interface SyntaxToken {
  /** The exact source text this token covers. */
  text: string;
  type: SyntaxTokenType;
}

/**
 * Highlights a single line of source. Tokens are contiguous and cover the
 * entire line (whitespace becomes `plain`), so concatenating `text` reproduces
 * the original line exactly. Designed to be called per rendered line only, so
 * huge bodies pay highlighting cost just for the visible viewport.
 */
export function highlightLine(
  line: string,
  language: SyntaxLanguage,
  csvColumnWidths: number[] = [],
): SyntaxToken[] {
  switch (language) {
    case 'json':
      return highlightJsonLine(line);
    case 'xml':
      return highlightXmlLine(line);
    case 'csv':
      return highlightCsvLine(line, csvColumnWidths);
    case 'text':
      return [{ text: line, type: 'plain' }];
  }
}
