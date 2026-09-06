export interface FeatureFlags {
  /** Render the experimental spreadsheet-style request list (V2). */
  useRequestListV2: boolean;
}

export const defaultFeatureFlags: FeatureFlags = {
  useRequestListV2: true,
};
