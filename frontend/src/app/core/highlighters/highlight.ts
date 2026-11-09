import { highlightJsonLine } from './json-highlighter';
import { highlightXmlLine } from './xml-highlighter';

/**
 * Languages the response body highlighter understands. `text` falls back to a
 * single plain token, leaving the body uncoloured.
 */
export type SyntaxLanguage = 'json' | 'xml' | 'text';

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
export function highlightLine(line: string, language: SyntaxLanguage): SyntaxToken[] {
  switch (language) {
    case 'json':
      return highlightJsonLine(line);
    case 'xml':
      return highlightXmlLine(line);
    case 'text':
      return [{ text: line, type: 'plain' }];
  }
}
