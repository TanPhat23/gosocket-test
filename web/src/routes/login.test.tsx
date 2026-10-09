import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Route } from './login';
import { isRedirect } from '@tanstack/react-router';
import type { QueryClient } from '@tanstack/react-query';

// Mock LoginForm to verify rendering
vi.mock('@/features/auth/components/LoginForm', () => ({
  LoginForm: () => <div data-testid="login-form">Mocked LoginForm</div>,
}));

describe('/login route', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('beforeLoad redirect guard', () => {
    it('does not throw redirect when user is unauthenticated (null)', async () => {
      const mockQueryClient = {
        ensureQueryData: vi.fn().mockResolvedValue(null),
      } as unknown as QueryClient;

      const beforeLoad = Route.options.beforeLoad;
      expect(beforeLoad).toBeDefined();

      await expect(
        beforeLoad!({
          context: { queryClient: mockQueryClient },
        } as any),
      ).resolves.toBeUndefined();

      expect(mockQueryClient.ensureQueryData).toHaveBeenCalledTimes(1);
    });

    it('throws redirect to "/" when user is authenticated', async () => {
      const mockUser = { id: 'usr-123', email: 'user@example.com' };
      const mockQueryClient = {
        ensureQueryData: vi.fn().mockResolvedValue(mockUser),
      } as unknown as QueryClient;

      const beforeLoad = Route.options.beforeLoad;
      expect(beforeLoad).toBeDefined();

      try {
        await beforeLoad!({
          context: { queryClient: mockQueryClient },
        } as any);
        expect.fail('Expected beforeLoad to throw a redirect');
      } catch (err) {
        expect(isRedirect(err)).toBe(true);
        const redirectObj = err as any;
        const target = redirectObj.options?.to || redirectObj.to || redirectObj.headers?.get?.('Location');
        expect(target).toBe('/');
      }

      expect(mockQueryClient.ensureQueryData).toHaveBeenCalledTimes(1);
    });
  });

  describe('component', () => {
    it('renders LoginForm component within container layout', () => {
      const Component = Route.options.component!;
      render(<Component />);
      expect(screen.getByTestId('login-form')).toBeInTheDocument();
      expect(screen.getByText('Mocked LoginForm')).toBeInTheDocument();
    });
  });
});
