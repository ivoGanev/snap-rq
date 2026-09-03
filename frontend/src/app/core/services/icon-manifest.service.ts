import { computed, Injectable, signal } from '@angular/core';

export interface IconEntry {
  id: string;
  path: string;
}

@Injectable({ providedIn: 'root' })
export class IconManifestService {
  readonly manifest = signal<Record<string, string>>({});
  readonly loaded = signal(false);
  readonly icons = computed<IconEntry[]>(() => {
    const map = this.manifest();
    return Object.entries(map).map(([id, filename]) => ({
      id,
      path: `/icons/${filename}`,
    }));
  });

  constructor() {
    this.load();
  }

  pathFor(iconId: string): string | undefined {
    const filename = this.manifest()[iconId];
    return filename ? `/icons/${filename}` : undefined;
  }

  private async load(): Promise<void> {
    try {
      const response = await fetch('/icons/_manifest.yaml');
      if (!response.ok) {
        console.error('Failed to load icon manifest:', response.status);
        return;
      }
      const text = await response.text();
      this.manifest.set(this.parseYaml(text));
      this.loaded.set(true);
    } catch (err) {
      console.error('Error loading icon manifest:', err);
    }
  }

  private parseYaml(text: string): Record<string, string> {
    const map: Record<string, string> = {};
    for (const raw of text.split(/\r?\n/)) {
      const line = raw.trim();
      if (!line || line.startsWith('#')) {
        continue;
      }
      const separator = line.indexOf(':');
      if (separator === -1) {
        continue;
      }
      const key = line.slice(0, separator).trim();
      const value = line
        .slice(separator + 1)
        .trim()
        .replace(/^["']|["']$/g, '');
      if (key) {
        map[key] = value;
      }
    }
    return map;
  }
}
