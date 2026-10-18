import '../index.css';
import type { ReactNode } from 'react';
import { createRootRouteWithContext, HeadContent, Outlet, Scripts } from '@tanstack/react-router';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { ThemeProvider } from '@/components/theme/ThemeProvider';
import { getQueryClient } from '@/lib/queryClient';

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Agent Task Manager' },
    ],
    links: [{ rel: 'icon', type: 'image/svg+xml', href: '/vite.svg' }],
  }),
  component: RootComponent,
});

function RootComponent() {
  return (
    <RootDocument>
      <QueryClientProvider client={getQueryClient()}>
        <ThemeProvider>
          <div className="min-h-screen bg-background text-foreground font-sans antialiased">
            <Outlet />
          </div>
        </ThemeProvider>
      </QueryClientProvider>
    </RootDocument>
  );
}

// RootDocument is the HTML shell every page renders into. It replaces the old index.html.
function RootDocument({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-slate-950 text-slate-100 antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
