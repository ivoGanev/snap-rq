export interface RequestColorOption {
  /** Hex colour stored in the database. */
  hex: string;
  /** Low-opacity tint used for the row background. */
  tint: string;
  label: string;
}

export const REQUEST_COLORS: readonly RequestColorOption[] = [
  { hex: '#AECBFA', tint: 'rgba(174, 203, 250, 0.18)', label: 'Pastel blue' },
  { hex: '#B6E2C6', tint: 'rgba(182, 226, 198, 0.18)', label: 'Pastel green' },
  { hex: '#F7E6A3', tint: 'rgba(247, 230, 163, 0.20)', label: 'Pastel yellow' },
];

/** Resolves a stored hex colour to its display metadata (null = no colour). */
export function requestColorInfo(hex: string | null): { hex: string; tint: string } | null {
  if (!hex) return null;
  const option = REQUEST_COLORS.find((c) => c.hex.toLowerCase() === hex.toLowerCase());
  return option ? { hex: option.hex, tint: option.tint } : { hex, tint: 'rgba(255,255,255,0.06)' };
}
