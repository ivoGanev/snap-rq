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
import { EditorState, type Extension, Compartment, Prec } from '@codemirror/state';
import {
  EditorView,
  keymap,
  lineNumbers,
  drawSelection,
  dropCursor,
  ViewPlugin,
  ViewUpdate,
  Decoration,
  DecorationSet,
  WidgetType,
} from '@codemirror/view';
import { json } from '@codemirror/lang-json';
import { linter, lintGutter, type Diagnostic } from '@codemirror/lint';
import { defaultKeymap, history, historyKeymap, redo } from '@codemirror/commands';

type BodyType = 'json' | 'text';

export interface BodyEditorVariable {
  key: string;
  value: string;
}

interface ValidationResult {
  valid: boolean;
  error: string;
}

const VARIABLE_QUOTED_REGEX = /"\{\{\s*(\w+)\s*\}\}"/g;
const VARIABLE_UNQUOTED_REGEX = /\{\{\s*(\w+)\s*\}\}/g;
const INDENT = '  ';

class VariableWidget extends WidgetType {
  constructor(
    private readonly key: string,
    private readonly value: string,
  ) {
    super();
  }

  toDOM(): HTMLElement {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'variable-chip';
    chip.textContent = this.key;
    chip.title = this.value;
    chip.tabIndex = -1;
    chip.addEventListener('mousedown', (event) => event.preventDefault());
    chip.addEventListener('focus', (event) => (event.target as HTMLElement).blur());
    return chip;
  }

  override eq(other: VariableWidget): boolean {
    return other.key === this.key && other.value === this.value;
  }
}

function variableDecorations(variables: BodyEditorVariable[]): Extension {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet = Decoration.none;

      constructor(view: EditorView) {
        this.decorations = this.buildDecorations(view);
      }

      update(update: ViewUpdate): void {
        if (update.docChanged || update.viewportChanged) {
          this.decorations = this.buildDecorations(update.view);
        }
      }

      private buildDecorations(view: EditorView): DecorationSet {
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
              widget: new VariableWidget(key, variable?.value ?? ''),
              inclusive: false,
              atomic: true,
            }).range(from, to),
          );
        }
        return decorations.length > 0 ? Decoration.set(decorations) : Decoration.none;
      }
    },
    { decorations: (value) => value.decorations },
  );
}

@Component({
  selector: 'app-body-editor',
  imports: [FormsModule],
  templateUrl: './body-editor.html',
  styleUrl: './body-editor.scss',
})
export class BodyEditor implements AfterViewInit, OnDestroy {
  readonly text = input<string>('');
  readonly type = input<BodyType>('json');
  readonly variables = input<BodyEditorVariable[]>([]);

  readonly textChange = output<string>();

  private readonly editorRef = viewChild<ElementRef<HTMLDivElement>>('editor');

  private view: EditorView | null = null;
  private readonly languageCompartment = new Compartment();
  private readonly variablesCompartment = new Compartment();
  private charWidth = 7.8;

  readonly isJson = computed(() => this.type() === 'json');
  readonly isValid = signal(true);
  readonly validationError = signal('');
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
      if (!this.view) return;
      this.view.dispatch({
        effects: this.variablesCompartment.reconfigure(variableDecorations(this.variables())),
      });
    });

    effect(() => {
      if (!this.view) return;
      this.view.dispatch({
        effects: this.languageCompartment.reconfigure(this.languageExtension(this.isJson())),
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
          this.variablesCompartment.of(variableDecorations(this.variables())),
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

  insertVariable(variable: BodyEditorVariable): void {
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
      drawSelection(),
      dropCursor(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      Prec.highest(
        keymap.of([
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
          const value = update.state.doc.toString();
          this.textChange.emit(value);
          this.validate(value);
        }
      }),
      this.customTheme(),
    ];
  }

  private languageExtension(isJson: boolean): Extension {
    if (!isJson) return [];
    return [json(), linter((view) => this.jsonLinter(view)), lintGutter()];
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
        },
        '.cm-content': {
          caretColor: 'var(--text)',
          padding: '0',
        },
        '.cm-gutters': {
          backgroundColor: 'transparent',
          color: 'var(--muted)',
          borderRight: '1px solid var(--glass-border)',
        },
        '.cm-lineNumbers .cm-gutterElement': {
          paddingLeft: '6px',
          paddingRight: '10px',
        },
        '.cm-activeLine': { backgroundColor: 'transparent' },
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
    const range = ranges.find((r) =>
      direction === 'backspace' ? r.to === pos : r.from === pos,
    );
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
    this.dropdownTop.set(Math.max(8, editorRect.top + (line.number * lineHeight) - this.view.scrollDOM.scrollTop + 4));
    this.dropdownLeft.set(Math.max(8, editorRect.left + column * this.charWidth - this.view.scrollDOM.scrollLeft));
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
    const raw = view.state.doc.toString().trim();
    if (!raw) return [];
    const { text } = this.replaceVariablesWithPlaceholders(raw);
    try {
      JSON.parse(text);
      return [];
    } catch (err) {
      return [
        {
          from: 0,
          to: view.state.doc.length,
          severity: 'error',
          message: (err as Error).message,
        },
      ];
    }
  }

  private validate(value: string): void {
    const result = this.validateForType(value, this.type());
    this.isValid.set(result.valid);
    this.validationError.set(result.error);
  }

  private validateForType(value: string, type: BodyType): ValidationResult {
    if (type === 'json') {
      return this.validateJson(value);
    }
    return { valid: true, error: '' };
  }

  private validateJson(value: string): ValidationResult {
    const trimmed = value.trim();
    if (!trimmed) {
      return { valid: true, error: '' };
    }
    const { text } = this.replaceVariablesWithPlaceholders(trimmed);
    try {
      JSON.parse(text);
      return { valid: true, error: '' };
    } catch (err) {
      return { valid: false, error: (err as Error).message };
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

  private restoreVariablesFromPlaceholders(value: string, placeholders: Map<string, string>): string {
    let result = value;
    for (const [token, original] of placeholders) {
      result = result.replaceAll(token, original);
    }
    return result;
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
