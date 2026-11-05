/**
 * Change-set model: the backend reports which component changed and with
 * which operation; the frontend turns that into the affected row ids and
 * fans it out to the stores.
 */

/** Raw payload of the backend 'command:changed' event. */
export interface CommandChanged {
  component: string; // "request"
  operation: string; // "delete"
  input: string; // JSON the command was invoked with
  result: string; // JSON the command returned
}

/** One affected entity: which store, which operation, which row ids. */
export interface EntityChange {
  component: string;
  operation: string;
  ids: number[];
}
