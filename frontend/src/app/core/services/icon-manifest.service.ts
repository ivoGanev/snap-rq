import { computed, effect, inject, Injectable, signal } from '@angular/core';
import { ThemeService } from './theme.service';
import type { IconReplacement } from './icon-api.service';

export interface IconEntry {
  id: string;
  path: string;
}

/** The reserved default icon id (shared by all packs). */
export const DEFAULT_ICON_ID = 'ico_1a9d4';

/** A feature's icon pack: immutable, versioned list of icon ids. */
export interface IconPack {
  version: number;
  icons: string[];
  replaces: Record<string, string>;
}

interface Line {
  indent: number;
  text: string;
}

@Injectable({ providedIn: 'root' })
export class IconManifestService {
  private readonly theme = inject(ThemeService);

  /** icon id -> filename (append-only, immutable ids). */
  readonly iconFiles = signal<Record<string, string>>({});
  /** feature name (collections/favourites/tags) -> pack. */
  readonly packs = signal<Record<string, IconPack>>({});
  readonly loaded = signal(false);

  private basePath = '/icons/default/';
  private loadGeneration = 0;
  private resolveReady!: () => void;

  /** Resolves once the manifest has been loaded (successfully or not). */
  readonly ready = new Promise<void>((resolve) => {
    this.resolveReady = resolve;
  });

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

  /** Icons available in the given feature's current pack, for the picker. */
  packIcons(feature: string): IconEntry[] {
    const pack = this.packs()[feature];
    if (!pack) return [];
    const files = this.iconFiles();
    return pack.icons
      .filter((id) => files[id])
      .map((id) => ({ id, path: this.resolvePath(files[id]) }));
  }

  /**
   * Resolves an icon id to its image path for the given feature, applying the
   * pack's `replaces` chain so old ids keep rendering the new artwork.
   */
  iconPath(feature: string, iconId: string): string | undefined {
    const pack = this.packs()[feature];
    const resolved = pack?.replaces[iconId] ?? iconId;
    const filename = this.iconFiles()[resolved];
    return filename ? this.resolvePath(filename) : undefined;
  }

  /** The union of every pack's replacements, for the one-time DB migration. */
  replacementPairs(): IconReplacement[] {
    const seen = new Set<string>();
    const pairs: IconReplacement[] = [];
    for (const pack of Object.values(this.packs())) {
      for (const [oldId, newId] of Object.entries(pack.replaces)) {
        if (!seen.has(oldId)) {
          seen.add(oldId);
          pairs.push({ old_id: oldId, new_id: newId });
        }
      }
    }
    return pairs;
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
      if (this.isStaleGeneration(generation)) return;
      if (!response.ok) {
        console.error('Failed to load icon manifest:', response.status);
        this.iconFiles.set({});
        this.packs.set({});
        this.loaded.set(false);
        this.resolveReady();
        return;
      }
      const text = await response.text();
      if (this.isStaleGeneration(generation)) return;
      const parsed = this.parseManifest(text);
      this.iconFiles.set(parsed.files);
      this.packs.set(parsed.packs);
      this.loaded.set(true);
      this.resolveReady();
    } catch (err) {
      if (this.isStaleGeneration(generation)) return;
      console.error('Error loading icon manifest:', err);
      this.iconFiles.set({});
      this.packs.set({});
      this.loaded.set(false);
      this.resolveReady();
    }
  }

  private parseManifest(text: string): {
    files: Record<string, string>;
    packs: Record<string, IconPack>;
  } {
    const files: Record<string, string> = {};
    const packs: Record<string, IconPack> = {};
    const lines = this.tokenize(text);

    let idx = 0;
    while (idx < lines.length) {
      const line = lines[idx];

      if (line.indent === 0 && line.text === 'icons:') {
        idx++;
        while (idx < lines.length && lines[idx].indent >= 2) {
          const entry = lines[idx];
          if (!entry.text.endsWith(':')) {
            idx++;
            continue;
          }
          const id = entry.text.slice(0, -1).trim();
          let file = '';
          idx++;
          while (idx < lines.length && lines[idx].indent > entry.indent) {
            const sep = lines[idx].text.indexOf(':');
            if (sep !== -1) {
              const key = lines[idx].text.slice(0, sep).trim();
              const value = this.unquote(lines[idx].text.slice(sep + 1).trim());
              if (key === 'file') file = value;
            }
            idx++;
          }
          if (file) files[id] = file;
        }
      } else if (line.indent === 0 && line.text === 'packs:') {
        idx++;
        while (idx < lines.length && lines[idx].indent >= 2) {
          const packLine = lines[idx];
          if (!packLine.text.endsWith(':')) {
            idx++;
            continue;
          }
          const feature = packLine.text.slice(0, -1).trim();
          const pack: IconPack = { version: 1, icons: [], replaces: {} };
          idx++;
          while (idx < lines.length && lines[idx].indent > packLine.indent) {
            const sub = lines[idx];
            if (sub.text === 'icons:') {
              idx++;
              while (idx < lines.length && lines[idx].indent > sub.indent) {
                const id = lines[idx].text.replace(/^-\s*/, '').trim();
                if (id) pack.icons.push(id);
                idx++;
              }
            } else if (sub.text === 'replaces:') {
              idx++;
              while (idx < lines.length && lines[idx].indent > sub.indent) {
                const sep = lines[idx].text.indexOf(':');
                if (sep !== -1) {
                  const oldId = lines[idx].text.slice(0, sep).trim();
                  const newId = lines[idx].text.slice(sep + 1).trim();
                  if (oldId && newId) pack.replaces[oldId] = newId;
                }
                idx++;
              }
            } else {
              const sep = sub.text.indexOf(':');
              if (sep !== -1 && sub.text.slice(0, sep).trim() === 'version') {
                pack.version = Number(sub.text.slice(sep + 1).trim()) || 1;
              }
              idx++;
            }
          }
          packs[feature] = pack;
        }
      } else {
        idx++;
      }
    }

    return { files, packs };
  }

  private tokenize(text: string): Line[] {
    return text
      .split(/\r?\n/)
      .map((raw) => raw.replace(/\s+$/, ''))
      .filter((raw) => raw.trim() && !raw.trim().startsWith('#'))
      .map((raw) => ({
        indent: raw.length - raw.trimStart().length,
        text: raw.trim(),
      }));
  }

  private unquote(value: string): string {
    return value.replace(/^["']|["']$/g, '');
  }
}
