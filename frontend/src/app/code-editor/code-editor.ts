import {
  AfterViewInit,
  Component,
  computed,
  effect,
  ElementRef,
  input,
  OnDestroy,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  EditorState,
  type Extension,
  Compartment,
  Prec,
  StateEffect,
  StateField,
} from '@codemirror/state';
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  drawSelection,
  dropCursor,
  ViewPlugin,
  ViewUpdate,
  Decoration,
  DecorationSet,
  WidgetType,
} from '@codemirror/view';
import { json } from '@codemirror/lang-json';
import { linter, type Diagnostic } from '@codemirror/lint';
import { defaultKeymap, history, historyKeymap, redo } from '@codemirror/commands';

export type EditorLanguage = 'json' | 'text';

export interface EditorVariable {
  key: string;
  value: string;
}

const setVariablesEffect = StateEffect.define<EditorVariable[]>({});

const variablesField = StateField.define<EditorVariable[]>({
  create: () => [],
  update: (value, tr) => {
    for (const effect of tr.effects) {
      if (effect.is(setVariablesEffect)) {
        value = effect.value;
      }
    }
    return value;
  },
});

const VARIABLE_QUOTED_REGEX = /"\{\{\s*(\w+)\s*\}\}"/g;
const VARIABLE_UNQUOTED_REGEX = /\{\{\s*(\w+)\s*\}\}/g;
const VARIABLE_REGEX = /"\{\{\s*(\w+)\s*\}\}"|\{\{\s*(\w+)\s*\}\}/g;
const INDENT = '  ';

interface LintReplacementRange {
  originalFrom: number;
  originalTo: number;
  replacedFrom: number;
  replacedTo: number;
}

class VariableWidget extends WidgetType {
  constructor(
    private readonly key: string,
    private readonly value: string,
    private readonly isMissing: boolean,
  ) {
    super();
  }

  toDOM(): HTMLElement {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = this.isMissing ? 'variable-chip is-missing' : 'variable-chip';
    chip.textContent = this.key;
    chip.title = this.isMissing ? 'not defined' : this.formatHint(this.value);
    chip.tabIndex = -1;
    if (this.isMissing) {
      chip.style.background = 'rgba(239, 68, 68, 0.8)';
    }
    chip.addEventListener('mousedown', (event) => event.preventDefault());
    chip.addEventListener('focus', (event) => (event.target as HTMLElement).blur());
    return chip;
  }

  override eq(other: VariableWidget): boolean {
    return (
      other.key === this.key && other.value === this.value && other.isMissing === this.isMissing
    );
  }

  private formatHint(value: string): string {
    return value === '' ? '""' : value;
  }
}

function variableDecorations(): Extension {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet = Decoration.none;

      constructor(view: EditorView) {
        this.decorations = this.buildDecorations(view);
      }

      update(update: ViewUpdate): void {
        const variablesChanged = update.transactions.some((tr) =>
          tr.effects.some((effect) => effect.is(setVariablesEffect)),
        );
        if (update.docChanged || update.viewportChanged || variablesChanged) {
          this.decorations = this.buildDecorations(update.view);
        }
      }

      private buildDecorations(view: EditorView): DecorationSet {
        const variables = view.state.field(variablesField);
        const decorations: Array<{ from: number; to: number; value: Decoration }> = [];
        const text = view.state.doc.toString();
        const regex = /\{\{\s*(\w+)\s*\}\}/g;
        let match: RegExpExecArray | null;
        while ((match = regex.exec(text)) !== null) {
          const key = match[1];
          const variable = variables.find((v) => v.key === key);
          const from = match.index;
          const to = from + match[0].length;
          decorations.push(
            Decoration.replace({
              widget: new VariableWidget(key, variable?.value ?? '', !variable),
              inclusive: false,
            }).range(from, to),
          );
        }
        return decorations.length > 0 ? Decoration.set(decorations) : Decoration.none;
      }
    },
    {
      decorations: (value) => value.decorations,
      provide: (plugin) =>
        EditorView.atomicRanges.of((view) => view.plugin(plugin)?.decorations ?? Decoration.none),
    },
  );
}

@Component({
  selector: 'app-code-editor',
  imports: [FormsModule],
  templateUrl: './code-editor.html',
  styleUrl: './code-editor.scss',
})
export class CodeEditor implements AfterViewInit, OnDestroy {
  readonly text = input<string>('');
  readonly language = input<EditorLanguage>('json');
  readonly variables = input<EditorVariable[]>([]);

  readonly textChange = output<string>();

  private readonly editorRef = viewChild<ElementRef<HTMLDivElement>>('editor');

  private view: EditorView | null = null;
  private readonly languageCompartment = new Compartment();
  private charWidth = 7.8;

  readonly isJson = computed(() => this.language() === 'json');
  readonly dropdownOpen = signal(false);
  readonly dropdownTop = signal(0);
  readonly dropdownLeft = signal(0);
  readonly dropdownIndex = signal(0);

  private readonly boundDropdownKeydown = this.onDropdownKeydown.bind(this);
  private readonly boundDocumentClick = this.onDocumentClick.bind(this);
  private readonly boundWindowResize = this.closeDropdown.bind(this);

  constructor() {
    effect(() => {
      const value = this.text();
      if (this.view && this.view.state.doc.toString() !== value) {
        this.view.dispatch({
          changes: { from: 0, to: this.view.state.doc.length, insert: value },
        });
      }
    });

    effect(() => {
      const vars = this.variables();
      if (!this.view) return;
      this.view.dispatch({ effects: setVariablesEffect.of(vars) });
    });

    effect(() => {
      const isJson = this.isJson();
      if (!this.view) return;
      this.view.dispatch({
        effects: this.languageCompartment.reconfigure(this.languageExtension(isJson)),
      });
    });
  }

  ngAfterViewInit(): void {
    const el = this.editorRef()?.nativeElement;
    if (!el) return;

    this.charWidth = this.measureCharWidth(el);

    this.view = new EditorView({
      parent: el,
      state: EditorState.create({
        doc: this.text(),
        extensions: [
          this.baseExtensions(),
          this.languageCompartment.of(this.languageExtension(this.isJson())),
          variablesField.init(() => this.variables()),
          variableDecorations(),
        ],
      }),
    });
  }

  ngOnDestroy(): void {
    this.view?.destroy();
    this.removeGlobalListeners();
  }

  formatCurrent(): void {
    if (!this.view || !this.isJson()) return;
    const raw = this.view.state.doc.toString();
    const formatted = this.formatJson(raw);
    if (formatted !== raw) {
      this.view.dispatch({
        changes: { from: 0, to: this.view.state.doc.length, insert: formatted },
      });
    }
    this.view.focus();
  }

  insertVariable(variable: EditorVariable): void {
    if (!this.view) return;
    const pos = this.view.state.selection.main.head;
    const insertion = `{{ ${variable.key} }}`;
    this.view.dispatch({
      changes: { from: pos, insert: insertion },
      selection: { anchor: pos + insertion.length },
    });
    this.view.focus();
    this.closeDropdown();
  }

  selectVariableByIndex(index: number): void {
    const variables = this.variables();
    if (index >= 0 && index < variables.length) {
      this.insertVariable(variables[index]);
    }
  }

  closeDropdown(): void {
    if (!this.dropdownOpen()) return;
    this.dropdownOpen.set(false);
    this.removeGlobalListeners();
  }

  private baseExtensions(): Extension {
    return [
      lineNumbers(),
      highlightActiveLine(),
      drawSelection(),
      dropCursor(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      Prec.highest(
        keymap.of([
          {
            mac: 'Cmd-Space',
            run: () => {
              this.openDropdown();
              return true;
            },
          },
          {
            key: 'Ctrl-Space',
            run: () => {
              this.openDropdown();
              return true;
            },
          },
          {
            key: 'Tab',
            run: (view) => {
              view.dispatch({
                changes: { from: view.state.selection.main.from, insert: INDENT },
                selection: { anchor: view.state.selection.main.from + INDENT.length },
              });
              return true;
            },
          },
          {
            key: 'Shift-Tab',
            run: (view) => this.outdentCurrentLine(view),
          },
          {
            key: 'Backspace',
            run: (view) => this.deleteAtomicRange(view, 'backspace'),
          },
          {
            key: 'Delete',
            run: (view) => this.deleteAtomicRange(view, 'delete'),
          },
          {
            key: 'Ctrl-Backspace',
            run: (view) => this.deleteAtomicRange(view, 'backspace'),
          },
          {
            key: 'Ctrl-Delete',
            run: (view) => this.deleteAtomicRange(view, 'delete'),
          },
          {
            key: 'Alt-Backspace',
            run: (view) => this.deleteAtomicRange(view, 'backspace'),
          },
          {
            key: 'Alt-Delete',
            run: (view) => this.deleteAtomicRange(view, 'delete'),
          },
          {
            key: 'Ctrl-ArrowLeft',
            run: (view) => this.moveOverVariable(view, false),
          },
          {
            key: 'Ctrl-ArrowRight',
            run: (view) => this.moveOverVariable(view, true),
          },
          {
            key: 'Alt-ArrowLeft',
            run: (view) => this.moveOverVariable(view, false),
          },
          {
            key: 'Alt-ArrowRight',
            run: (view) => this.moveOverVariable(view, true),
          },
        ]),
      ),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          this.textChange.emit(update.state.doc.toString());
        }
      }),
      this.customTheme(),
    ];
  }

  private languageExtension(isJson: boolean): Extension {
    if (!isJson) return [];
    return [json(), linter((view) => this.jsonLinter(view), { delay: 100 })];
  }

  private customTheme(): Extension {
    return EditorView.theme(
      {
        '&': {
          color: 'var(--text)',
          backgroundColor: 'transparent',
          fontFamily:
            "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
          fontSize: '0.8125rem',
          lineHeight: '1.5',
          height: '100%',
          minHeight: '0',
        },
        '.cm-content': {
          caretColor: 'var(--text)',
          padding: 'var(--s-2) var(--s-3)',
        },
        '.cm-scroller': {
          flex: '1 1 auto',
          minHeight: '0',
          overflowX: 'auto',
          overflowY: 'auto',
        },
        '.cm-gutters': {
          backgroundColor: 'transparent',
          color: 'var(--muted)',
          borderRight: 'none',
        },
        '.cm-lineNumbers .cm-gutterElement': {
          paddingLeft: '6px',
          paddingRight: '10px',
        },
        '.cm-activeLine': { backgroundColor: 'transparent' },
        '.cm-focused .cm-activeLine': { backgroundColor: 'var(--hover-bg)' },
        '.cm-selectionBackground': { background: 'var(--selected-bg)' },
        '.cm-focused .cm-selectionBackground': { background: 'var(--selected-bg)' },
        '.cm-cursor': { borderLeftColor: 'var(--text)' },
        '.cm-tooltip': {
          background: 'var(--surface-solid)',
          border: '1px solid var(--glass-border)',
          borderRadius: 'var(--radius)',
        },
        '.cm-tooltip-lint': {
          background: 'var(--surface-solid)',
        },
        '.cm-lint-marker': {
          color: 'var(--error-text)',
        },
      },
      { dark: true },
    );
  }

  private outdentCurrentLine(view: EditorView): boolean {
    const pos = view.state.selection.main.head;
    const line = view.state.doc.lineAt(pos);
    const text = line.text;
    const match = text.match(/^(  |\t)/);
    if (!match) return false;
    view.dispatch({
      changes: { from: line.from, to: line.from + match[0].length, insert: '' },
      selection: { anchor: Math.max(line.from, pos - match[0].length) },
    });
    return true;
  }

  private moveOverVariable(view: EditorView, forward: boolean): boolean {
    const ranges = this.getVariableRanges(view.state.doc);
    if (ranges.length === 0) return false;

    const pos = view.state.selection.main.head;
    const range = ranges.find((r) => (forward ? r.from === pos : r.to === pos));
    if (!range) return false;

    const target = forward ? range.to : range.from;
    view.dispatch({ selection: { anchor: target }, scrollIntoView: true });
    return true;
  }

  private deleteAtomicRange(view: EditorView, direction: 'backspace' | 'delete'): boolean {
    const selection = view.state.selection.main;
    if (!selection.empty) return false;

    const ranges = this.getVariableRanges(view.state.doc);
    if (ranges.length === 0) return false;

    const pos = selection.head;
    const range = ranges.find((r) => (direction === 'backspace' ? r.to === pos : r.from === pos));
    if (!range) return false;

    view.dispatch({
      changes: { from: range.from, to: range.to, insert: '' },
      selection: { anchor: range.from },
    });
    return true;
  }

  private getVariableRanges(doc: { toString(): string }): { from: number; to: number }[] {
    const ranges: { from: number; to: number }[] = [];
    const text = doc.toString();
    const regex = /\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      ranges.push({ from: match.index, to: match.index + match[0].length });
    }
    return ranges;
  }

  private onDropdownKeydown(event: KeyboardEvent): void {
    if (!this.dropdownOpen()) return;

    const variables = this.variables();

    if (event.code === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.closeDropdown();
      return;
    }

    if (event.code === 'ArrowDown') {
      event.preventDefault();
      event.stopPropagation();
      if (variables.length > 0) {
        this.dropdownIndex.update((i) => Math.min(i + 1, variables.length - 1));
      }
      return;
    }

    if (event.code === 'ArrowUp') {
      event.preventDefault();
      event.stopPropagation();
      if (variables.length > 0) {
        this.dropdownIndex.update((i) => Math.max(i - 1, 0));
      }
      return;
    }

    if (event.code === 'Enter') {
      event.preventDefault();
      event.stopPropagation();
      if (variables.length > 0) {
        this.selectVariableByIndex(this.dropdownIndex());
      } else {
        this.closeDropdown();
      }
      return;
    }
  }

  private openDropdown(): void {
    const variables = this.variables();
    this.dropdownIndex.set(variables.length > 0 ? 0 : -1);
    this.dropdownOpen.set(true);
    this.computeDropdownPosition();
    this.addGlobalListeners();
  }

  private computeDropdownPosition(): void {
    if (!this.view) return;
    const pos = this.view.state.selection.main.head;
    const coords = this.view.coordsAtPos(pos);
    if (coords) {
      this.dropdownTop.set(Math.max(8, coords.bottom + 4));
      this.dropdownLeft.set(Math.max(8, coords.left));
      return;
    }

    // Fallback when coordsAtPos cannot determine the caret rectangle.
    const editorRect = this.view.dom.getBoundingClientRect();
    const line = this.view.state.doc.lineAt(pos);
    const lineHeight = this.parseLineHeight(this.view.dom);
    const column = pos - line.from;
    this.dropdownTop.set(
      Math.max(8, editorRect.top + line.number * lineHeight - this.view.scrollDOM.scrollTop + 4),
    );
    this.dropdownLeft.set(
      Math.max(8, editorRect.left + column * this.charWidth - this.view.scrollDOM.scrollLeft),
    );
  }

  private parseLineHeight(element: HTMLElement): number {
    const raw = getComputedStyle(element).lineHeight;
    const parsed = parseFloat(raw);
    return isNaN(parsed) ? 20 : parsed;
  }

  private measureCharWidth(element: HTMLElement): number {
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');
    if (!ctx) return 7.8;
    const style = getComputedStyle(element);
    ctx.font = `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
    return ctx.measureText('M').width;
  }

  private jsonLinter(view: EditorView): Diagnostic[] {
    const raw = view.state.doc.toString();
    if (!raw.trim()) return [];
    const { text, ranges } = this.replaceVariablesForLint(raw);
    try {
      JSON.parse(text);
      return [];
    } catch (err) {
      const message = (err as Error).message;
      const match = /position (\d+)/i.exec(message);
      const replacedPos = match ? parseInt(match[1], 10) : 0;
      const pos = this.mapReplacedToOriginal(replacedPos, ranges);
      const len = view.state.doc.length;
      const from = Math.max(0, Math.min(pos, len - 1));
      const to = Math.min(from + 1, len);
      return [{ from, to, severity: 'error', message }];
    }
  }

  private formatJson(value: string): string {
    const { text, placeholders } = this.replaceVariablesWithPlaceholders(value);
    try {
      const formatted = JSON.stringify(JSON.parse(text), null, 2);
      return this.restoreVariablesFromPlaceholders(formatted, placeholders);
    } catch {
      return value;
    }
  }

  private replaceVariablesWithPlaceholders(value: string): {
    text: string;
    placeholders: Map<string, string>;
  } {
    const placeholders = new Map<string, string>();
    let counter = 0;

    let text = value.replace(VARIABLE_QUOTED_REGEX, (match: string) => {
      const token = `"__VAR_Q_${counter++}__"`;
      placeholders.set(token, match);
      return token;
    });

    text = text.replace(VARIABLE_UNQUOTED_REGEX, (match: string) => {
      const token = `"__VAR_U_${counter++}__"`;
      placeholders.set(token, match);
      return token;
    });

    return { text, placeholders };
  }

  private restoreVariablesFromPlaceholders(
    value: string,
    placeholders: Map<string, string>,
  ): string {
    let result = value;
    for (const [token, original] of placeholders) {
      result = result.replaceAll(token, original);
    }
    return result;
  }

  private replaceVariablesForLint(value: string): {
    text: string;
    ranges: LintReplacementRange[];
  } {
    const ranges: LintReplacementRange[] = [];
    let result = '';
    let lastIndex = 0;
    let counterQ = 0;
    let counterU = 0;

    VARIABLE_REGEX.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = VARIABLE_REGEX.exec(value)) !== null) {
      const original = match[0];
      const isQuoted = original.startsWith('"');
      const token = isQuoted ? `"__VAR_Q_${counterQ++}__"` : `"__VAR_U_${counterU++}__"`;
      const originalFrom = match.index;
      const originalTo = originalFrom + original.length;
      const replacedFrom = result.length + (originalFrom - lastIndex);
      const replacedTo = replacedFrom + token.length;

      result += value.slice(lastIndex, originalFrom);
      result += token;
      ranges.push({ originalFrom, originalTo, replacedFrom, replacedTo });
      lastIndex = originalTo;
    }
    result += value.slice(lastIndex);
    return { text: result, ranges };
  }

  private mapReplacedToOriginal(pos: number, ranges: LintReplacementRange[]): number {
    let originalPos = pos;
    for (const range of ranges) {
      if (pos <= range.replacedFrom) break;
      if (pos >= range.replacedTo) {
        originalPos += range.originalTo - range.originalFrom - (range.replacedTo - range.replacedFrom);
      } else {
        return range.originalFrom + (pos - range.replacedFrom);
      }
    }
    return originalPos;
  }

  private onDocumentClick(event: MouseEvent): void {
    const dropdown = (event.target as HTMLElement)?.closest('.variable-dropdown');
    if (!dropdown) {
      this.closeDropdown();
    }
  }

  private addGlobalListeners(): void {
    window.addEventListener('keydown', this.boundDropdownKeydown, true);
    document.addEventListener('click', this.boundDocumentClick);
    window.addEventListener('resize', this.boundWindowResize);
  }

  private removeGlobalListeners(): void {
    window.removeEventListener('keydown', this.boundDropdownKeydown, true);
    document.removeEventListener('click', this.boundDocumentClick);
    window.removeEventListener('resize', this.boundWindowResize);
  }
}
