import { createRouter } from '@tanstack/react-router';
import { routeTree } from './routeTree.gen';
import { getQueryClient } from './lib/queryClient';

// getRouter is the factory TanStack Start calls on the server and in the browser.
export function getRouter() {
  const queryClient = getQueryClient();

  return createRouter({
    routeTree,
    context: {
      queryClient,
    },
    defaultPreload: 'intent',
    scrollRestoration: true,
  });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
