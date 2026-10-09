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

    // Serve static assets from dist
    let filePath = `./dist${url.pathname}`;
    let file = Bun.file(filePath);
    if (await file.exists()) {
      return new Response(file);
    }

    // SPA fallback to index.html for client routing
    const indexFile = Bun.file('./dist/index.html');
    if (await indexFile.exists()) {
      return new Response(indexFile, {
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
