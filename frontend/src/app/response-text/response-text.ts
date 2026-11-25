import { Component, DestroyRef, ElementRef, afterNextRender, effect, inject, input } from '@angular/core';
import { EditorState, RangeSetBuilder, type Extension } from '@codemirror/state';
import {
  Decoration,
  EditorView,
  ViewPlugin,
  keymap,
  lineNumbers,
  type DecorationSet,
  type ViewUpdate,
} from '@codemirror/view';
import { codeFolding, foldGutter, foldKeymap } from '@codemirror/language';
import { json } from '@codemirror/lang-json';
import { xml } from '@codemirror/lang-xml';
import { highlightLine, type SyntaxLanguage } from '../core/highlighters/highlight';

const tokenMarks = new Map<string, Decoration>();
function tokenMark(type: string): Decoration {
  let mark = tokenMarks.get(type);
  if (!mark) {
    mark = Decoration.mark({ class: `tok-${type}` });
    tokenMarks.set(type, mark);
  }
  return mark;
}

/**
 * Colours the lines on screen with the app's own highlighters, so the body
 * looks the same for every language; the language packages only add folding.
 */
function tokenColours(language: SyntaxLanguage, csvWidths: number[]): Extension {
  const build = (view: EditorView): DecorationSet => {
    const builder = new RangeSetBuilder<Decoration>();
    for (const { from, to } of view.visibleRanges) {
      for (let pos = from; pos <= to; ) {
        const line = view.state.doc.lineAt(pos);
        let at = line.from;
        for (const token of highlightLine(line.text, language, csvWidths)) {
          const end = at + token.text.length;
          if (token.type !== 'plain' && end > at) builder.add(at, end, tokenMark(token.type));
          at = end;
        }
        pos = line.to + 1;
      }
    }
    return builder.finish();
  };
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;
      constructor(view: EditorView) {
        this.decorations = build(view);
      }
      update(update: ViewUpdate): void {
        if (update.docChanged || update.viewportChanged) this.decorations = build(update.view);
      }
    },
    { decorations: (plugin) => plugin.decorations },
  );
}

// CodeMirror builds its DOM itself, so component-scoped styles can't reach it.
const theme = EditorView.theme({
  '&': {
    color: 'var(--text)',
    backgroundColor: 'transparent',
    fontSize: '0.8125rem',
    flex: '1 1 auto',
    minHeight: '0',
  },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': {
    fontFamily: 'var(--font-mono)',
    lineHeight: '1.5',
    overflow: 'auto',
  },
  '.cm-content': { padding: '0' },
  '.cm-line': { padding: '0 var(--s-2)' },
  // Solid, so text scrolled sideways passes under it unseen.
  '.cm-gutters': {
    background: 'linear-gradient(var(--input-bg), var(--input-bg)), var(--surface-solid)',
    color: 'var(--muted)',
    border: 'none',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    minWidth: '3ch',
    padding: '0 var(--s-1) 0 0',
    opacity: '0.7',
  },
  '.cm-foldGutter .cm-gutterElement': {
    width: '14px',
    cursor: 'pointer',
  },
  '.cm-foldGutter .cm-gutterElement:hover': { color: 'var(--text)' },
  '.cm-foldPlaceholder': {
    margin: '0 2px',
    padding: '0 4px',
    color: 'var(--muted)',
    backgroundColor: 'var(--hover-highlight)',
    border: 'none',
    borderRadius: '3px',
  },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': {
    backgroundColor: 'var(--selected-bg) !important',
  },
  '.tok-key': { color: 'var(--syntax-key)' },
  '.tok-string': { color: 'var(--syntax-string)' },
  '.tok-number': { color: 'var(--syntax-number)' },
  '.tok-boolean': { color: 'var(--syntax-boolean)' },
  '.tok-null': { color: 'var(--syntax-null)' },
  '.tok-punctuation': { color: 'var(--syntax-punctuation)' },
  '.tok-tag': { color: 'var(--syntax-tag)' },
  '.tok-attribute': { color: 'var(--syntax-attribute)' },
  '.tok-comment': { color: 'var(--syntax-comment)' },
  '.tok-column-even': { color: 'var(--syntax-csv-even)' },
  '.tok-column-odd': { color: 'var(--syntax-csv-odd)' },
});

function folding(language: SyntaxLanguage): Extension[] {
  if (language === 'json') return [json()];
  if (language === 'xml') return [xml()];
  return [];
}

@Component({
  selector: 'app-response-text',
  template: '',
  styles: ':host { display: flex; flex-direction: column; min-width: 0; }',
})
export class ResponseText {
  readonly text = input.required<string>();
  readonly language = input<SyntaxLanguage>('text');
  readonly csvWidths = input<number[]>([]);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private view: EditorView | null = null;

  constructor() {
    afterNextRender(() => {
      this.view = new EditorView({ parent: this.host.nativeElement, state: this.createState() });
    });
    effect(() => {
      const state = this.createState();
      this.view?.setState(state);
    });
    inject(DestroyRef).onDestroy(() => this.view?.destroy());
  }

  private createState(): EditorState {
    const language = this.language();
    return EditorState.create({
      doc: this.text(),
      extensions: [
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        EditorView.contentAttributes.of({ tabindex: '0' }),
        lineNumbers(),
        codeFolding(),
        foldGutter({ openText: '▾', closedText: '▸' }),
        keymap.of(foldKeymap),
        ...folding(language),
        tokenColours(language, this.csvWidths()),
        theme,
      ],
    });
  }
}
