# Snap RQ

Your #1 API testing partner!

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

TODO: restore large-scale random data generation if still needed. The seed script now targets the mock collection only.

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