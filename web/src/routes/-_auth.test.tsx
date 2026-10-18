import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Route } from './_auth';
import { isRedirect } from '@tanstack/react-router';
import type { QueryClient } from '@tanstack/react-query';

describe('/_auth layout route guard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('beforeLoad redirect guard', () => {
    it('throws redirect to /login with search.redirect when unauthenticated (null)', async () => {
      const mockQueryClient = {
        ensureQueryData: vi.fn().mockResolvedValue(null),
      } as unknown as QueryClient;

      const beforeLoad = Route.options.beforeLoad;
      expect(beforeLoad).toBeDefined();

      const mockLocation = { href: '/dashboard/test' };

      try {
        await beforeLoad!({
          context: { queryClient: mockQueryClient },
          location: mockLocation,
        } as any);
        expect.fail('Expected beforeLoad to throw a redirect');
      } catch (err) {
        expect(isRedirect(err)).toBe(true);
        const redirectObj = err as any;
        const target = redirectObj.options?.to || redirectObj.to || redirectObj.headers?.get?.('Location');
        expect(target).toBe('/login');
        const search = redirectObj.options?.search || redirectObj.search;
        expect(search).toEqual({ redirect: '/dashboard/test' });
      }

      expect(mockQueryClient.ensureQueryData).toHaveBeenCalledTimes(1);
    });

    it('returns user context when authenticated', async () => {
      const mockUser = { id: 'usr-456', email: 'auth@example.com' };
      const mockQueryClient = {
        ensureQueryData: vi.fn().mockResolvedValue(mockUser),
      } as unknown as QueryClient;

      const beforeLoad = Route.options.beforeLoad;
      expect(beforeLoad).toBeDefined();

      const result = await beforeLoad!({
        context: { queryClient: mockQueryClient },
        location: { href: '/' },
      } as any);

      expect(result).toEqual({ user: mockUser });
      expect(mockQueryClient.ensureQueryData).toHaveBeenCalledTimes(1);
    });
  });
});
