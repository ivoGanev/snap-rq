// A tiny local HTTP server for the end-to-end tests to call, so they never
// depend on the internet. Usage: node stub-server.mjs <port>
//
//   /ok             200 {"ok":true}
//   /slow?ms=1500   200 after the given delay
//   /status/404     that status code
//   /echo           200 {"headers": the request's headers}
//   /secrets        200 a login response: secrets in the body, a token and a session cookie in the headers
//   /long           200 a long JSON list with one very wide line and a secret far down
import { createServer } from 'node:http';

const port = Number(process.argv[2] ?? 9872);

createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://stub');
  const reply = (status) => {
    res.writeHead(status, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ status, path: url.pathname }));
  };

  if (url.pathname === '/ok') return reply(200);
  if (url.pathname === '/echo') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ headers: req.headers }));
    return;
  }
  if (url.pathname === '/secrets') {
    res.writeHead(200, {
      'content-type': 'application/json',
      'x-request-id': 'req-0001',
      'x-auth-token': 'tok-header-4',
      'set-cookie': 'session_id=sess-5; Path=/; HttpOnly',
    });
    res.end(
      JSON.stringify({
        user: { id: 42, username: 'demo-user', password: 'hunter2' },
        access_token: 'tok-access-1',
        expires_in: 3600,
      }),
    );
    return;
  }
  if (url.pathname === '/long') {
    const items = Array.from({ length: 300 }, (_, id) => ({ id, note: `item ${id}` }));
    items[5].note = 'wide '.repeat(200);
    items[200] = { id: 200, access_token: 'tok-deep-7' };
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ items }));
    return;
  }
  if (url.pathname === '/slow') {
    setTimeout(() => reply(200), Number(url.searchParams.get('ms') ?? 1000));
    return;
  }
  const status = /^\/status\/(\d{3})$/.exec(url.pathname);
  if (status) return reply(Number(status[1]));
  reply(404);
}).listen(port, '127.0.0.1');
