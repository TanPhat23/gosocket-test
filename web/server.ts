const PORT = Number(process.env.PORT) || 3000;
const API_URL = process.env.INTERNAL_API_URL || 'http://control-plane:8080';

const server = Bun.serve({
  port: PORT,
  async fetch(req) {
    const url = new URL(req.url);

    // Proxy API and WebSockets to Go control plane
    if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws')) {
      const targetUrl = new URL(url.pathname + url.search, API_URL);
      const headers = new Headers(req.headers);
      headers.set('host', new URL(API_URL).host);

      return fetch(targetUrl.toString(), {
        method: req.method,
        headers,
        body: req.body,
      });
    }

    // Serve the TanStack Start client build (dist/client) for assets and files.
    const asset = Bun.file(`./dist/client${url.pathname}`);
    if (url.pathname !== '/' && (await asset.exists())) {
      return new Response(asset);
    }

    // SPA fallback: every other path gets the Start shell, which boots the client router.
    const shell = Bun.file('./dist/client/_shell.html');
    if (await shell.exists()) {
      return new Response(shell, {
        headers: {
          'Content-Type': 'text/html; charset=utf-8',
          'Cache-Control': 'no-cache',
        },
      });
    }

    return new Response('Not Found', { status: 404 });
  },
});

console.log(`🚀 Bun server running at http://0.0.0.0:${server.port}`);
