import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Dialogs } from '@wailsio/runtime';
import { CodeEditor, type EditorVariable, type EditorLanguage } from '../code-editor/code-editor';

export type BodyKind = 'json' | 'text' | 'form-data';

/** One row of a multipart form body. */
export interface FormBodyEntry {
  key: string;
  type: 'text' | 'multiline' | 'file';
  value: string;
  file_path: string;
  file_name: string;
  enabled: boolean;
}

function parseForm(raw: string): FormBodyEntry[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as FormBodyEntry[]) : [];
  } catch {
    return [];
  }
}

function basename(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

function normalize(entries: FormBodyEntry[]): FormBodyEntry[] {
  return entries.map((e) =>
    e.type === 'file' && !e.file_name ? { ...e, file_name: basename(e.file_path) } : e,
  );
}

/**
 * Shared body editor UI used by both the full request editor and the bottom
 * drawer body tab. Renders a CodeMirror editor for json/text, and a key/value
 * table for form-data.
 */
@Component({
  selector: 'app-request-body-editor',
  imports: [FormsModule, CodeEditor],
  templateUrl: './request-body-editor.html',
  styleUrl: './request-body-editor.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestBodyEditor {
  readonly body = input<string>('');
  readonly type = input<BodyKind>('json');
  readonly bodyForm = input<string>('');
  readonly variables = input<EditorVariable[]>([]);
  readonly showLabel = input<boolean>(true);

  readonly bodyChange = output<string>();
  readonly typeChange = output<BodyKind>();
  readonly formChange = output<string>();

  private readonly codeEditor = viewChild(CodeEditor);

  readonly formRows = signal<FormBodyEntry[]>([]);

  readonly codeLanguage = computed<EditorLanguage>(() =>
    this.type() === 'text' ? 'text' : 'json',
  );

  constructor() {
    effect(() => this.formRows.set(normalize(parseForm(this.bodyForm()))));
  }

  setType(type: BodyKind): void {
    this.typeChange.emit(type);
  }

  formatCurrent(): void {
    this.codeEditor()?.formatCurrent();
  }

  onTextChange(text: string): void {
    this.bodyChange.emit(text);
  }

  // ---- form-data row editing ----

  private patch(rows: FormBodyEntry[]): void {
    this.formRows.set(rows);
    this.formChange.emit(JSON.stringify(rows));
  }

  addRow(): void {
    this.patch([...this.formRows(), { key: '', type: 'text', value: '', file_path: '', file_name: '', enabled: true }]);
  }

  removeRow(index: number): void {
    const rows = [...this.formRows()];
    rows.splice(index, 1);
    this.patch(rows);
  }

  setRow(index: number, change: Partial<FormBodyEntry>): void {
    const rows = this.formRows().map((row, i) => (i === index ? { ...row, ...change } : row));
    this.patch(rows);
  }

  async pickFile(index: number): Promise<void> {
    const path = await Dialogs.OpenFile({ Title: 'Choose file', AllowsMultipleSelection: false });
    if (typeof path === 'string' && path) {
      this.setRow(index, { type: 'file', file_path: path, file_name: basename(path), value: '' });
    }
  }

  // ---- multi-line value dialog ----

  readonly multilineOpen = signal(false);
  readonly multilineIndex = signal<number | null>(null);
  readonly multilineValue = signal('');

  openMultiline(index: number): void {
    this.multilineIndex.set(index);
    this.multilineValue.set(this.formRows()[index]?.value ?? '');
    this.multilineOpen.set(true);
  }

  closeMultiline(): void {
    this.multilineOpen.set(false);
    this.multilineIndex.set(null);
    this.multilineValue.set('');
  }

  saveMultiline(): void {
    const index = this.multilineIndex();
    if (index !== null) {
      this.setRow(index, { value: this.multilineValue() });
    }
    this.closeMultiline();
  }
}
