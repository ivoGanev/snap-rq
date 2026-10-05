import { computed, effect, inject, Injectable, signal } from '@angular/core';
import { ThemeService } from './theme.service';

export interface IconEntry {
  id: string;
  path: string;
}

@Injectable({ providedIn: 'root' })
export class IconManifestService {
  private readonly theme = inject(ThemeService);

  readonly manifest = signal<Record<string, string>>({});
  readonly loaded = signal(false);
  readonly icons = computed<IconEntry[]>(() => {
    const map = this.manifest();
    return Object.entries(map).map(([id, filename]) => ({
      id,
      path: this.resolvePath(filename),
    }));
  });

  private basePath = '/icons/default/';
  private loadGeneration = 0;

  constructor() {
    effect(() => {
      const manifestPath = this.theme.iconSetPath();
      this.basePath = this.deriveBasePath(manifestPath);
      this.load(manifestPath);
    });
  }

  private isStaleGeneration(generation: number): boolean {
    return generation !== this.loadGeneration;
  }

  pathFor(iconId: string): string | undefined {
    const filename = this.manifest()[iconId];
    return filename ? this.resolvePath(filename) : undefined;
  }

  private resolvePath(filename: string): string {
    return `${this.basePath}${filename}`;
  }

  private deriveBasePath(manifestPath: string): string {
    const lastSlash = manifestPath.lastIndexOf('/');
    return lastSlash === -1 ? '' : manifestPath.slice(0, lastSlash + 1);
  }

  private async load(manifestPath: string): Promise<void> {
    const generation = ++this.loadGeneration;
    try {
      const response = await fetch(manifestPath);
      if (this.isStaleGeneration(generation)) {
        return;
      }
      if (!response.ok) {
        console.error('Failed to load icon manifest:', response.status);
        this.manifest.set({});
        this.loaded.set(false);
        return;
      }
      const text = await response.text();
      if (this.isStaleGeneration(generation)) {
        return;
      }
      this.manifest.set(this.parseYaml(text));
      this.loaded.set(true);
    } catch (err) {
      if (this.isStaleGeneration(generation)) {
        return;
      }
      console.error('Error loading icon manifest:', err);
      this.manifest.set({});
      this.loaded.set(false);
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
