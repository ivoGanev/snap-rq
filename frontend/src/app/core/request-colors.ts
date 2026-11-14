/**
 * Request colour slots. The database stores only a slot name, never a
 * colour: each theme defines what a slot looks like through the
 * `--label-<slot>` CSS custom properties, so recolouring a theme never
 * touches stored data. Keep this list in sync with `ColorSlots` in
 * backend/models/colorslot.go.
 */
export const REQUEST_COLOR_SLOTS = [
  'primary',
  'secondary',
  'tertiary',
  'quaternary',
  'quinary',
  'senary',
] as const;

export type RequestColorSlot = (typeof REQUEST_COLOR_SLOTS)[number];

export interface RequestColorOption {
  /** Slot name stored in the database. */
  slot: RequestColorSlot;
  /** Theme-neutral label: the slot's position, never a colour name. */
  label: string;
  /** CSS value that resolves to the slot's colour in the active theme. */
  cssVar: string;
}

export const REQUEST_COLORS: readonly RequestColorOption[] = REQUEST_COLOR_SLOTS.map((slot, index) => ({
  slot,
  label: `Color ${index + 1}`,
  cssVar: `var(--label-${slot})`,
}));

export function isRequestColorSlot(value: string | null): value is RequestColorSlot {
  return value !== null && (REQUEST_COLOR_SLOTS as readonly string[]).includes(value);
}

/** Resolves a stored slot to the CSS the UI paints with (null = no colour). */
export function requestColorInfo(slot: string | null): { cssVar: string; tint: string } | null {
  if (!isRequestColorSlot(slot)) return null;
  const cssVar = `var(--label-${slot})`;
  return { cssVar, tint: `color-mix(in srgb, ${cssVar} 18%, transparent)` };
}
