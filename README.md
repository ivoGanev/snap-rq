# Snap RQ

Your #1 REST API testing client!

## Why Snap-RQ?

1. User-efficient workflow
2. No vendor/cloud lock-in
3. Free to use
4. Custom themes

## Tech stack

- **Backend:** Go (Wails v3)
- **Frontend:** Angular
- **Database:** SQLite

## How to run in dev mode (Wails)

1. Navigate to your project directory in the terminal.

2. To run your application in development mode, use the following command:

   ```
   wails3 dev
   ```

   This will start your application and enable hot-reloading for both frontend and backend changes.

3. To build your application for production, use:

   ```
   wails3 build
   ```

   This will create a production-ready executable in the `build` directory.

## Rebuilding bindings
On few occasions, we may have to rebuild our bindings

- wails3 generate bindings -ts -i -clean=true

## Seed mock collection

`tools/seed_scale_test.py` creates an idempotent **Mock Server** collection in the app's database, pre-filled with one request per mock-server endpoint (content-type mocks, status mocks, delay/empty, and echo).

```
python tools/seed_scale_test.py
```

Re-running the script will not duplicate the collection. Use `--force` to recreate it:

```
python tools/seed_scale_test.py --force
```

The collection is given an orange color appearance so it stands out from real API data.

## Scale test

`tools/seed_scale_test.py` now creates three kinds of data by default:

- the fixed `Mock Server` collection for endpoint coverage
- 30 randomized load-test collections, each with hundreds of generated requests containing realistic URLs, methods, bodies, request headers, response headers, and status codes
- a dedicated `Edge Cases` collection with malformed JSON, bad headers, missing auth, oversized payloads, and other intentionally broken request patterns

The defaults are already set for a large load, so you can simply run:

```
python tools/seed_scale_test.py
```

The random seed is reproducible, and you can override the size with:

```
python tools/seed_scale_test.py --random-collections 30 --requests-per-collection 500 --seed 1337
```

Use `--force` to rebuild the mock collection, the random test data, and the edge-case collection from scratch.

## Mock server

Lives in `tools/mock-server`

A small standalone HTTP server for manually testing the client against different response formats and status codes. It includes:

- Content-type mocks: `/mock/json`, `/mock/csv`, `/mock/html`, `/mock/text`, `/mock/xml`, `/mock/binary`
- Status mocks: `/mock/status/200`, `/mock/status/201`, `/mock/status/204`, `/mock/status/400`, `/mock/status/401`, `/mock/status/404`, `/mock/status/500`
- Special mocks: `/mock/delay?ms=2000`, `/mock/empty`
- Echo endpoint: `/echo` (preserved from the original echo service)

Run it with:

```
cd tools/mock-server
go run .
```

This is not a replacement for automated client tests.

## License

Snap-rq is licensed under the GNU General Public License v3.0 or later (GPL-3.0-or-later).