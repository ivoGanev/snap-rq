import { inject, Injectable, signal } from '@angular/core';
import { parse } from 'yaml';
import { ThemeService } from './theme.service';

export interface IconEntry {
  id: string;
  path: string;
}

export interface IconDefinition {
  file: string;
  label?: string;
}

/** One version of a pack: icon id -> definition. */
export type IconPackVersion = Record<string, IconDefinition>;

/** A named pack with immutable versions. */
export interface IconPack {
  versions: Record<number, IconPackVersion>;
}

/** The reserved default icon id (present in every pack version). */
export const DEFAULT_ICON_ID = 'ico_1a9d4';

const MANIFEST_PATH = '/icons/_manifest.yaml';
const FEATURES = ['collections', 'favourites', 'tags'] as const;
const FALLBACK_PACK = 'standard';

/** Shape of the manifest, as parsed by the `yaml` library. */
interface IconManifestDocument {
  id?: string[];
  packs?: Record<string, { versions?: Record<string, IconPackVersion> }>;
}

@Injectable({ providedIn: 'root' })
export class IconManifestService {
  private readonly theme = inject(ThemeService);

  readonly packs = signal<Record<string, IconPack>>({});
  readonly loaded = signal(false);

  private idRegistry = new Set<string>();
  private resolveReady!: () => void;
  private rejectReady!: (err: unknown) => void;

  /** Resolves once the manifest is loaded AND validated; rejects otherwise. */
  readonly ready = new Promise<void>((resolve, reject) => {
    this.resolveReady = resolve;
    this.rejectReady = reject;
  });

  constructor() {
    void this.init();
  }

  /** The full set of currently valid icon ids, for DB cleanup. */
  validIconIds(): string[] {
    return [...this.idRegistry];
  }

  /** Icons available in the given feature's active pack, for the picker. */
  packIcons(feature: string): IconEntry[] {
    const resolved = this.resolve(feature);
    if (!resolved) return [];
    const version = this.packs()[resolved.pack]?.versions[resolved.version];
    if (!version) return [];
    return Object.entries(version).map(([id, def]) => ({
      id,
      path: this.resolvePath(def.file),
    }));
  }

  /**
   * Resolves an icon id to its image path for the given feature, using the
   * active pack/version. Missing ids fall back to the standard pack, then to
   * the default icon, so a legacy or themed omission never renders nothing.
   */
  iconPath(feature: string, iconId: string): string | undefined {
    const resolved = this.resolve(feature);
    const filename =
      (resolved && this.fileFor(resolved.pack, resolved.version, iconId)) ||
      this.fileFor(FALLBACK_PACK, this.latestVersion(FALLBACK_PACK), iconId) ||
      this.fileFor(FALLBACK_PACK, this.latestVersion(FALLBACK_PACK), DEFAULT_ICON_ID);
    return filename ? this.resolvePath(filename) : undefined;
  }

  /** Resolves the active pack + version for a feature from the current theme. */
  resolve(feature: string): { pack: string; version: number } | null {
    const entry = this.theme.currentTheme()?.iconSet?.features?.[feature];
    return entry ?? null;
  }

  private fileFor(packName: string, version: number, iconId: string): string | undefined {
    return this.packs()[packName]?.versions[version]?.[iconId]?.file;
  }

  private latestVersion(packName: string): number {
    const versions = this.packs()[packName]?.versions;
    if (!versions) return 1;
    const keys = Object.keys(versions).map(Number);
    return keys.length > 0 ? Math.max(...keys) : 1;
  }

  private async init(): Promise<void> {
    try {
      await this.theme.ready;

      const response = await fetch(MANIFEST_PATH);
      if (!response.ok) {
        throw new Error(`Failed to load icon manifest: ${response.status}`);
      }
      const text = await response.text();
      const doc = parse(text) as IconManifestDocument;

      this.idRegistry = new Set(doc.id ?? []);
      this.packs.set(this.normalizePacks(doc.packs));

      this.validate();
      this.loaded.set(true);
      this.resolveReady();
    } catch (err) {
      console.error('Icon registry validation failed:', err);
      this.rejectReady(err);
    }
  }

  private normalizePacks(
    packs?: Record<string, { versions?: Record<string, IconPackVersion> }>,
  ): Record<string, IconPack> {
    const result: Record<string, IconPack> = {};
    for (const [name, pack] of Object.entries(packs ?? {})) {
      const versions: Record<number, IconPackVersion> = {};
      for (const [version, icons] of Object.entries(pack.versions ?? {})) {
        versions[Number(version)] = icons;
      }
      result[name] = { versions };
    }
    return result;
  }

  /**
   * The app cannot start without a correct registry, so any inconsistency
   * throws here (surfaced through the APP_INITIALIZER).
   */
  private validate(): void {
    // 1. Every icon id referenced in any pack version is in the registry, and
    //    every pack version includes the default icon.
    for (const [packName, pack] of Object.entries(this.packs())) {
      for (const [version, icons] of Object.entries(pack.versions)) {
        for (const id of Object.keys(icons)) {
          if (!this.idRegistry.has(id)) {
            throw new Error(`icon pack ${packName}@${version} references unknown id ${id}`);
          }
        }
        if (!(DEFAULT_ICON_ID in icons)) {
          throw new Error(`icon pack ${packName}@${version} is missing the default icon ${DEFAULT_ICON_ID}`);
        }
      }
    }

    // 2. Every theme must map every feature to an existing pack + version.
    const themes = this.theme.registry();
    if (themes.length === 0) {
      throw new Error('no themes registered');
    }
    for (const theme of themes) {
      for (const feature of FEATURES) {
        const entry = theme.iconSet?.features?.[feature];
        if (!entry) {
          throw new Error(`theme ${theme.id} is missing a pack for feature ${feature}`);
        }
        const pack = this.packs()[entry.pack];
        if (!pack) {
          throw new Error(`theme ${theme.id} feature ${feature} references unknown pack ${entry.pack}`);
        }
        if (!pack.versions[entry.version]) {
          throw new Error(
            `theme ${theme.id} feature ${feature} references unknown version ${entry.pack}@${entry.version}`,
          );
        }
      }
    }
  }

  private resolvePath(filename: string): string {
    return `/icons/${filename}`;
  }
}
