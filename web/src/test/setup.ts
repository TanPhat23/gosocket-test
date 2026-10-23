import '@testing-library/jest-dom/vitest';
import React from 'react';
import { vi } from 'vitest';
import { QueryClientProvider, QueryClient } from '@tanstack/react-query';

// Ensure all React Testing Library renders have a QueryClientProvider if none is present
vi.mock('@testing-library/react', async (importOriginal: () => Promise<typeof import('@testing-library/react')>) => {
  const actual = await importOriginal();
  return {
    ...actual,
    render: (ui: React.ReactNode, options?: any) => {
      const testQueryClient = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      });
      const Wrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
        const content = options?.wrapper
          ? React.createElement(options.wrapper, null, children)
          : children;
        return React.createElement(
          QueryClientProvider,
          { client: testQueryClient },
          content
        );
      };
      return actual.render(ui, {
        ...options,
        wrapper: Wrapper,
      });
    },
  };
});

// Polyfill window.matchMedia for JSDOM test environments
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

// Polyfill localStorage for Node/JSDOM test environments if absent
if (typeof window !== 'undefined' && (!window.localStorage || typeof window.localStorage.getItem !== 'function')) {
  const store: Record<string, string> = {};
  const mockStorage = {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => {
      store[key] = String(value);
    },
    removeItem: (key: string) => {
      delete store[key];
    },
    clear: () => {
      Object.keys(store).forEach((k) => delete store[k]);
    },
    key: (i: number) => Object.keys(store)[i] ?? null,
    get length() {
      return Object.keys(store).length;
    },
  };

  Object.defineProperty(window, 'localStorage', {
    value: mockStorage,
    writable: true,
  });
  Object.defineProperty(globalThis, 'localStorage', {
    value: mockStorage,
    writable: true,
  });
}

// Polyfill ResizeObserver for JSDOM test environments (used by cmdk / base-ui)
if (typeof globalThis.ResizeObserver === 'undefined') {
  class MockResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = MockResizeObserver as any;
  if (typeof window !== 'undefined') {
    (window as any).ResizeObserver = MockResizeObserver;
  }
}

// Polyfill Element.prototype.scrollIntoView for JSDOM test environments (used by cmdk)
if (typeof Element !== 'undefined' && typeof Element.prototype.scrollIntoView !== 'function') {
  Element.prototype.scrollIntoView = () => {};
}

// Monaco needs a real browser. Tests get a plain element that carries the same text, so the editor's content is
// still checkable without loading the editor.
vi.mock('@monaco-editor/react', async () => {
  const React = await import('react');
  return {
    Editor: ({ value }: { value?: string }) => React.createElement('pre', { 'data-testid': 'monaco-editor' }, value ?? ''),
  };
});
vi.mock('@/lib/monaco', () => ({ configureMonaco: () => {} }));
