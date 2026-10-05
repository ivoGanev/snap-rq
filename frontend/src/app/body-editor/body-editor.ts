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

type BodyType = 'json' | 'text';

export interface BodyEditorVariable {
  key: string;
  value: string;
}

interface ValidationResult {
  valid: boolean;
  error: string;
}

interface EditorState {
  value: string;
}

const MAX_HISTORY_SIZE = 100;
const INDENT = '  ';
const VARIABLE_QUOTED_REGEX = /"\{\{\s*(\w+)\s*\}\}"/g;
const VARIABLE_UNQUOTED_REGEX = /\{\{\s*(\w+)\s*\}\}/g;

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

  readonly isJson = computed(() => this.type() === 'json');
  readonly isValid = signal(true);
  readonly validationError = signal('');
  readonly dropdownOpen = signal(false);
  readonly dropdownTop = signal(0);
  readonly dropdownLeft = signal(0);
  readonly dropdownIndex = signal(0);

  private readonly boundDocumentClick = this.onDocumentClick.bind(this);
  private readonly boundWindowResize = this.closeDropdown.bind(this);

  private lastState: EditorState | null = null;
  private readonly undoStack: EditorState[] = [];
  private readonly redoStack: EditorState[] = [];
  private isRestoringState = false;
  private charWidth = 7.8;

  constructor() {
    effect(() => {
      const value = this.text();
      const editor = this.editorRef()?.nativeElement;
      if (editor) {
        const currentRaw = this.readRawFromEditor();
        if (currentRaw !== value) {
          this.renderRaw(value, editor);
          this.resetHistory();
        }
      }
      this.lastState = { value };
      this.validate(value);
    });
  }

  ngAfterViewInit(): void {
    const editor = this.editorRef()?.nativeElement;
    if (editor) {
      this.charWidth = this.measureCharWidth(editor);
      this.renderRaw(this.text(), editor);
      this.lastState = { value: this.text() };
    }
  }

  ngOnDestroy(): void {
    this.removeGlobalListeners();
  }

  onInput(): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;
    const raw = this.readRawFromEditor();

    if (!this.isRestoringState) {
      this.pushUndo(this.lastState);
      this.redoStack.length = 0;
    }
    this.lastState = { value: raw };
    this.textChange.emit(raw);
    this.validate(raw);
  }

  onKeydown(event: KeyboardEvent): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;

    if (event.ctrlKey && event.code === 'Space') {
      event.preventDefault();
      this.openDropdown();
      return;
    }

    if (this.dropdownOpen() && this.handleDropdownKey(event)) {
      return;
    }

    if (event.key === 'Tab') {
      event.preventDefault();
      this.insertTextAtCursor(INDENT);
      this.syncAfterEdit();
      return;
    }

    if (event.code === 'Enter') {
      event.preventDefault();
      this.insertTextAtCursor('\n');
      this.syncAfterEdit();
      return;
    }

    if (event.ctrlKey && event.code === 'KeyZ') {
      event.preventDefault();
      if (event.shiftKey) {
        this.redo();
      } else {
        this.undo();
      }
      return;
    }

    if (event.ctrlKey && event.code === 'KeyY') {
      event.preventDefault();
      this.redo();
    }
  }

  private handleDropdownKey(event: KeyboardEvent): boolean {
    const variables = this.variables();

    if (event.code === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.closeDropdown();
      return true;
    }

    if (variables.length === 0) {
      if (event.code === 'Enter') {
        event.preventDefault();
        event.stopPropagation();
        this.closeDropdown();
        return true;
      }
      return false;
    }

    switch (event.code) {
      case 'ArrowDown':
        event.preventDefault();
        event.stopPropagation();
        this.dropdownIndex.update((i) => Math.min(i + 1, variables.length - 1));
        return true;
      case 'ArrowUp':
        event.preventDefault();
        event.stopPropagation();
        this.dropdownIndex.update((i) => Math.max(i - 1, 0));
        return true;
      case 'Enter':
        event.preventDefault();
        event.stopPropagation();
        this.selectVariableByIndex(this.dropdownIndex());
        return true;
    }

    return false;
  }

  onPaste(event: ClipboardEvent): void {
    event.preventDefault();
    const pasted = event.clipboardData?.getData('text/plain') ?? '';
    this.insertTextAtCursor(pasted);
    this.syncAfterEdit();
  }

  formatCurrent(): void {
    if (!this.isJson()) return;
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;
    const raw = this.readRawFromEditor();
    const formatted = this.formatJson(raw);
    if (formatted !== raw) {
      this.pushUndo(this.lastState);
      this.redoStack.length = 0;
      this.renderRaw(formatted, editor);
      this.lastState = { value: formatted };
      this.textChange.emit(formatted);
    }
    this.validate(formatted);
    editor.focus();
  }

  insertVariable(variable: BodyEditorVariable): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;

    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) return;
    const range = selection.getRangeAt(0);
    range.deleteContents();
    const chip = this.createChip(variable);
    range.insertNode(chip);
    range.setStartAfter(chip);
    range.setEndAfter(chip);
    selection.removeAllRanges();
    selection.addRange(range);

    this.syncAfterEdit();
    this.closeDropdown();
  }

  selectVariableByIndex(index: number): void {
    const variables = this.variables();
    if (index >= 0 && index < variables.length) {
      this.insertVariable(variables[index]);
    }
  }

  private undo(): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor || this.undoStack.length === 0) return;
    this.redoStack.push(this.captureState(editor));
    const state = this.undoStack.pop()!;
    this.restoreState(editor, state);
  }

  private redo(): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor || this.redoStack.length === 0) return;
    this.undoStack.push(this.captureState(editor));
    const state = this.redoStack.pop()!;
    this.restoreState(editor, state);
  }

  private restoreState(editor: HTMLDivElement, state: EditorState): void {
    this.isRestoringState = true;
    this.renderRaw(state.value, editor);
    this.lastState = state;
    this.textChange.emit(state.value);
    this.validate(state.value);
    this.isRestoringState = false;
    editor.focus();
  }

  private syncAfterEdit(): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;
    const raw = this.readRawFromEditor();
    this.pushUndo(this.lastState);
    this.redoStack.length = 0;
    this.lastState = { value: raw };
    this.textChange.emit(raw);
    this.validate(raw);
  }

  private captureState(editor: HTMLDivElement): EditorState {
    return { value: this.readRawFromEditor() };
  }

  private pushUndo(state: EditorState | null): void {
    if (!state) return;
    const last = this.undoStack[this.undoStack.length - 1];
    if (last && last.value === state.value) return;
    this.undoStack.push(state);
    if (this.undoStack.length > MAX_HISTORY_SIZE) {
      this.undoStack.shift();
    }
  }

  private resetHistory(): void {
    this.undoStack.length = 0;
    this.redoStack.length = 0;
  }

  private createChip(variable: BodyEditorVariable): HTMLButtonElement {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'variable-chip';
    chip.contentEditable = 'false';
    chip.textContent = variable.key;
    chip.dataset['value'] = variable.value;
    chip.title = variable.value;
    chip.tabIndex = -1;
    return chip;
  }

  private renderRaw(raw: string, editor: HTMLDivElement): void {
    editor.innerHTML = '';
    const fragment = document.createDocumentFragment();
    let lastIndex = 0;
    let match: RegExpExecArray | null;

    VARIABLE_UNQUOTED_REGEX.lastIndex = 0;
    while ((match = VARIABLE_UNQUOTED_REGEX.exec(raw)) !== null) {
      if (match.index > lastIndex) {
        fragment.appendChild(document.createTextNode(raw.slice(lastIndex, match.index)));
      }
      const key = match[1];
      const variable = this.variables().find((v) => v.key === key);
      fragment.appendChild(this.createChip(variable ?? { key, value: '' }));
      lastIndex = VARIABLE_UNQUOTED_REGEX.lastIndex;
    }

    if (lastIndex < raw.length) {
      fragment.appendChild(document.createTextNode(raw.slice(lastIndex)));
    }

    if (raw.length === 0) {
      fragment.appendChild(document.createTextNode(''));
    }

    editor.appendChild(fragment);
  }

  private readRawFromEditor(): string {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return '';
    let raw = '';
    for (const node of Array.from(editor.childNodes)) {
      raw += this.readRawFromNode(node);
    }
    return raw;
  }

  private readRawFromNode(node: Node): string {
    if (node.nodeType === Node.TEXT_NODE) {
      return node.textContent ?? '';
    }
    if (node instanceof HTMLElement && node.classList.contains('variable-chip')) {
      return `{{ ${node.textContent ?? ''} }}`;
    }
    if (node instanceof HTMLElement) {
      let text = '';
      for (const child of Array.from(node.childNodes)) {
        text += this.readRawFromNode(child);
      }
      if (node.tagName === 'BR') {
        text = '\n';
      }
      if (node.tagName === 'DIV' && !text.endsWith('\n')) {
        text += '\n';
      }
      return text;
    }
    return node.textContent ?? '';
  }

  private insertTextAtCursor(text: string): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;
    editor.focus();

    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) {
      editor.appendChild(document.createTextNode(text));
      return;
    }

    try {
      if (document.queryCommandSupported('insertText')) {
        const inserted = document.execCommand('insertText', false, text);
        if (inserted) return;
      }
    } catch {
      // fall through to manual insertion
    }

    const range = selection.getRangeAt(0);
    range.deleteContents();
    const node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.setEndAfter(node);
    selection.removeAllRanges();
    selection.addRange(range);
  }

  private openDropdown(): void {
    const variables = this.variables();
    this.dropdownIndex.set(variables.length > 0 ? 0 : -1);
    this.dropdownOpen.set(true);
    this.computeDropdownPosition();
    this.addGlobalListeners();
  }

  closeDropdown(): void {
    if (!this.dropdownOpen()) return;
    this.dropdownOpen.set(false);
    this.removeGlobalListeners();
  }

  private computeDropdownPosition(): void {
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return;
    const coords = this.getCaretCoordinates(editor);
    this.dropdownTop.set(Math.max(8, coords.top));
    this.dropdownLeft.set(Math.max(8, coords.left));
  }

  private getCaretCoordinates(editor: HTMLDivElement): { top: number; left: number } {
    const selection = window.getSelection();
    if (selection && selection.rangeCount > 0) {
      const range = selection.getRangeAt(0);
      const rect = range.getBoundingClientRect();
      if (rect.width > 0 || rect.height > 0) {
        return { top: rect.bottom + 4, left: rect.left };
      }
    }

    // Fallback for collapsed ranges or when the browser returns a zero rect.
    const rawBefore = this.getTextBeforeCaret();
    const lines = rawBefore.split('\n');
    const lineIndex = lines.length - 1;
    const columnIndex = lines[lineIndex]?.length ?? 0;
    const editorRect = editor.getBoundingClientRect();
    const lineHeight = this.parseLineHeight(editor);

    return {
      top: editorRect.top + (lineIndex + 1) * lineHeight - editor.scrollTop + 4,
      left: editorRect.left + columnIndex * this.charWidth - editor.scrollLeft,
    };
  }

  private getTextBeforeCaret(): string {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) return '';
    const range = selection.getRangeAt(0);
    const preCaretRange = range.cloneRange();
    const editor = this.editorRef()?.nativeElement;
    if (!editor) return '';
    preCaretRange.selectNodeContents(editor);
    preCaretRange.setEnd(range.startContainer, range.startOffset);
    return this.domToRawText(preCaretRange.cloneContents());
  }

  private domToRawText(fragment: DocumentFragment): string {
    let raw = '';
    for (const node of Array.from(fragment.childNodes)) {
      raw += this.readRawFromNode(node);
    }
    return raw;
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

    // First replace already-quoted variables: "{{ key }}" -> "__VAR_Q_N__"
    let text = value.replace(VARIABLE_QUOTED_REGEX, (match: string) => {
      const token = `"__VAR_Q_${counter++}__"`;
      placeholders.set(token, match);
      return token;
    });

    // Then replace bare variables: {{ key }} -> "__VAR_U_N__"
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
    document.addEventListener('click', this.boundDocumentClick);
    window.addEventListener('resize', this.boundWindowResize);
  }

  private removeGlobalListeners(): void {
    document.removeEventListener('click', this.boundDocumentClick);
    window.removeEventListener('resize', this.boundWindowResize);
  }
}
