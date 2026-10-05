import { computed, Injectable, signal } from '@angular/core';

export interface Theme {
  id: string;
  name: string;
  css: string;
  iconSet: string;
  fontFamily: string;
  preview: string;
}

const STORAGE_KEY = 'theme:id';
const LINK_ID = 'theme-css';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  readonly registry = signal<Theme[]>([]);
  readonly loaded = signal(false);
  readonly currentTheme = signal<Theme | null>(null);
  readonly iconSetPath = computed(() => this.currentTheme()?.iconSet ?? '/icons/default/_manifest.yaml');

  constructor() {
    this.load();
  }

  select(themeId: string): void {
    const theme = this.registry().find((t) => t.id === themeId);
    if (theme) {
      this.apply(theme);
    }
  }

  private async load(): Promise<void> {
    try {
      const response = await fetch('/themes/themes.json');
      if (!response.ok) {
        console.error('Failed to load theme registry:', response.status);
        return;
      }
      const themes: Theme[] = await response.json();
      this.registry.set(themes);

      const savedId = localStorage.getItem(STORAGE_KEY);
      const initial = themes.find((t) => t.id === savedId) ?? themes[0] ?? null;
      if (initial) {
        this.apply(initial);
      }
      this.loaded.set(true);
    } catch (err) {
      console.error('Error loading theme registry:', err);
    }
  }

  private apply(theme: Theme): void {
    this.currentTheme.set(theme);
    localStorage.setItem(STORAGE_KEY, theme.id);

    let link = document.getElementById(LINK_ID) as HTMLLinkElement | null;
    if (!link) {
      link = document.createElement('link');
      link.id = LINK_ID;
      link.rel = 'stylesheet';
      document.head.appendChild(link);
    }
    link.href = theme.css;

    document.documentElement.setAttribute('data-theme', theme.id);
    document.documentElement.style.fontFamily = theme.fontFamily;
  }
}
