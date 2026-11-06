/**
 * Change-set model: the backend reports which operation ran and which rows
 * it changed; the frontend fans that out to the stores.
 */

/** One kind of row a command changed. */
export interface RowChange {
  component: string; // "request"
  ids: number[]; // [] when the store should reload its whole list
}

/** Raw payload of the backend 'command:changed' event. */
export interface CommandChanged {
  operation: string; // "delete"
  changes: RowChange[];
}

/** One changed entity: which store, which operation, which row ids. */
export interface EntityChange {
  component: string;
  operation: string;
  ids: number[];
}
