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

## Scale test

There is a seed script in tools/seed_scale_test.py. Run this when you want to scale test the app.
TODO: Need to make this populate on a specific profile so we don't drop the database.
```
python tools/seed_scale_test.py
```

## Echo service

Lives in tools/echo-service

A simple echo service to check quickly if the client works. This is not a replacement for automated client tests.