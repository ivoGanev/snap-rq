import type { SyntaxToken } from './highlight';

const NAME_RE = /^[^\s=<>/'"]+/;
const WHITESPACE_RE = /\s/;

/**
 * Single-pass, per-line XML highlighter.
 *
 * Recognises tags (with attribute names and quoted values), comments, CDATA
 * sections, processing instructions and text nodes. Highlighting is line-local:
 * comments/CDATA that span multiple lines are coloured only on their opening
 * line, which keeps this compatible with virtual scrolling at no per-document
 * cost.
 */
export function highlightXmlLine(line: string): SyntaxToken[] {
  const tokens: SyntaxToken[] = [];
  let i = 0;

  while (i < line.length) {
    const ch = line[i];

    if (ch === '<') {
      if (line.startsWith('<!--', i)) {
        const end = line.indexOf('-->', i + 4);
        const upto = end === -1 ? line.length : end + 3;
        tokens.push({ text: line.slice(i, upto), type: 'comment' });
        i = upto;
        continue;
      }

      if (line.startsWith('<![CDATA[', i)) {
        const end = line.indexOf(']]>', i + 9);
        const upto = end === -1 ? line.length : end + 3;
        tokens.push({ text: line.slice(i, upto), type: 'string' });
        i = upto;
        continue;
      }

      if (line.startsWith('<?', i)) {
        const end = line.indexOf('?>', i + 2);
        const upto = end === -1 ? line.length : end + 2;
        tokens.push({ text: line.slice(i, upto), type: 'tag' });
        i = upto;
        continue;
      }

      const gt = line.indexOf('>', i + 1);
      const upto = gt === -1 ? line.length : gt + 1;
      tokens.push(...highlightTag(line.slice(i, upto)));
      i = upto;
      continue;
    }

    const nextLt = line.indexOf('<', i);
    const upto = nextLt === -1 ? line.length : nextLt;
    if (upto > i) {
      tokens.push({ text: line.slice(i, upto), type: 'plain' });
    }
    i = upto;
  }

  return tokens;
}

function highlightTag(content: string): SyntaxToken[] {
  const tokens: SyntaxToken[] = [];
  let i = 0;

  while (i < content.length) {
    const ch = content[i];

    if (ch === '"' || ch === "'") {
      const quote = ch;
      let j = i + 1;
      while (j < content.length && content[j] !== quote) {
        j++;
      }
      const end = j < content.length ? j + 1 : content.length;
      tokens.push({ text: content.slice(i, end), type: 'string' });
      i = end;
      continue;
    }

    if (ch === '<' || ch === '>' || ch === '/' || ch === '=') {
      tokens.push({ text: ch, type: 'tag' });
      i++;
      continue;
    }

    if (WHITESPACE_RE.test(ch)) {
      let j = i;
      while (j < content.length && WHITESPACE_RE.test(content[j])) {
        j++;
      }
      tokens.push({ text: content.slice(i, j), type: 'plain' });
      i = j;
      continue;
    }

    const match = NAME_RE.exec(content.slice(i));
    const name = match ? match[0] : ch;
    let k = i + name.length;
    while (k < content.length && WHITESPACE_RE.test(content[k])) {
      k++;
    }
    const isAttribute = content[k] === '=';
    tokens.push({ text: name, type: isAttribute ? 'attribute' : 'tag' });
    i += name.length;
  }

  return tokens;
}
