// A tiny local HTTP server for the end-to-end tests to call, so they never
// depend on the internet. Usage: node stub-server.mjs <port>
//
//   /ok             200 {"ok":true}
//   /slow?ms=1500   200 after the given delay
//   /status/404     that status code
import { createServer } from 'node:http';

const port = Number(process.argv[2] ?? 9872);

createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://stub');
  const reply = (status) => {
    res.writeHead(status, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ status, path: url.pathname }));
  };

  if (url.pathname === '/ok') return reply(200);
  if (url.pathname === '/slow') {
    setTimeout(() => reply(200), Number(url.searchParams.get('ms') ?? 1000));
    return;
  }
  const status = /^\/status\/(\d{3})$/.exec(url.pathname);
  if (status) return reply(Number(status[1]));
  reply(404);
}).listen(port, '127.0.0.1');
