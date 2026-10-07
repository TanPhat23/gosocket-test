import { describe, it, expect, vi, beforeEach } from 'vitest';
import { authApi } from './authApi';
import { axiosClient } from '@/lib/axios';

vi.mock('@/lib/axios', () => ({
  axiosClient: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

describe('authApi', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('getMe', () => {
    it('parses nested user payload { user: { ... } } returned by backend', async () => {
      const mockPayload = {
        user: {
          id: 'user_123',
          issuer: 'https://issuer.example.com',
          subject: 'sub_123',
          email: 'alice@example.com',
          display_name: 'Alice Developer',
        },
      };

      vi.mocked(axiosClient.get).mockResolvedValueOnce({ data: mockPayload });

      const result = await authApi.getMe();
      expect(axiosClient.get).toHaveBeenCalledWith('/auth/me');
      expect(result).toEqual({
        id: 'user_123',
        issuer: 'https://issuer.example.com',
        subject: 'sub_123',
        email: 'alice@example.com',
        display_name: 'Alice Developer',
      });
    });

    it('parses direct flat user payload { id: "..." }', async () => {
      const mockPayload = {
        id: 'user_456',
        issuer: null,
        subject: null,
        email: 'bob@example.com',
        display_name: 'Bob Admin',
      };

      vi.mocked(axiosClient.get).mockResolvedValueOnce({ data: mockPayload });

      const result = await authApi.getMe();
      expect(axiosClient.get).toHaveBeenCalledWith('/auth/me');
      expect(result).toEqual({
        id: 'user_456',
        issuer: null,
        subject: null,
        email: 'bob@example.com',
        display_name: 'Bob Admin',
      });
    });

    it('handles nullable and optional fields correctly', async () => {
      const mockPayload = {
        user: {
          id: 'user_minimal',
        },
      };

      vi.mocked(axiosClient.get).mockResolvedValueOnce({ data: mockPayload });

      const result = await authApi.getMe();
      expect(result).toEqual({
        id: 'user_minimal',
      });
    });

    it('throws validation error if id is missing', async () => {
      vi.mocked(axiosClient.get).mockResolvedValueOnce({
        data: {
          user: {
            email: 'missing-id@example.com',
          },
        },
      });

      await expect(authApi.getMe()).rejects.toThrow();
    });
  });

  describe('logout', () => {
    it('calls POST /auth/logout and returns status', async () => {
      vi.mocked(axiosClient.post).mockResolvedValueOnce({ data: { status: 'logged out' } });

      const res = await authApi.logout();
      expect(axiosClient.post).toHaveBeenCalledWith('/auth/logout');
      expect(res).toEqual({ status: 'logged out' });
    });
  });
});
