import { spawn } from 'node:child_process';
import { createWriteStream, existsSync, mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { once } from 'node:events';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { APP_BINARY, APP_PORT, APP_URL } from './env';

/**
 * Starts the app in server mode on a fresh, empty data folder, and returns
 * the teardown: stop the app, then delete the folder (database, backups,
 * key file). The app is stopped first because Windows can't delete an open
 * database. Its output goes to test-results/app.log.
 */
export default async function globalSetup(): Promise<() => Promise<void>> {
  if (!existsSync(APP_BINARY)) {
    throw new Error(`${APP_BINARY} is missing: run \`task test:e2e\` from the repository root`);
  }

  const dataDir = mkdtempSync(join(tmpdir(), 'snaprq-e2e-'));
  const logDir = resolve(__dirname, '..', '..', 'test-results');
  mkdirSync(logDir, { recursive: true });
  const log = createWriteStream(join(logDir, 'app.log'));

  const app = spawn(APP_BINARY, [], {
    env: {
      ...process.env,
      WAILS_SERVER_HOST: 'localhost',
      WAILS_SERVER_PORT: String(APP_PORT),
      SNAPRQ_DATA_DIR: dataDir,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  app.stdout.pipe(log);
  app.stderr.pipe(log);
  const exited = once(app, 'exit');

  try {
    await waitUntilServing(APP_URL, exited);
  } catch (err) {
    app.kill();
    rmSync(dataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    throw err;
  }

  return async () => {
    app.kill();
    await exited;
    log.end();
    rmSync(dataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
  };
}

async function waitUntilServing(url: string, exited: Promise<unknown>, timeoutMs = 30_000): Promise<void> {
  let gone = false;
  void exited.then(() => (gone = true));
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (gone) throw new Error('the app exited while starting: see test-results/app.log');
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`the app did not answer on ${url} within ${timeoutMs / 1000}s`);
}
