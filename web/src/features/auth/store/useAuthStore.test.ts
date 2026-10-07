import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useAuthStore } from './useAuthStore';
import { authApi } from '../api/authApi';

vi.mock('../api/authApi', () => ({
  authApi: {
    getMe: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
  },
}));

describe('useAuthStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: null,
      isLoading: true,
      isInitialized: false,
      error: null,
    });
  });

  it('initializes with default state', () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isLoading).toBe(true);
    expect(state.isInitialized).toBe(false);
    expect(state.error).toBeNull();
  });

  describe('login', () => {
    it('sets user, isLoading=false, isInitialized=true, error=null on success', async () => {
      const mockUser = {
        id: 'usr-admin',
        email: 'admin@example.com',
        display_name: 'Admin',
      };
      vi.mocked(authApi.login).mockResolvedValueOnce(mockUser);

      await useAuthStore.getState().login({ email: 'admin@example.com', password: 'password123' });

      const state = useAuthStore.getState();
      expect(state.user).toEqual(mockUser);
      expect(state.isLoading).toBe(false);
      expect(state.isInitialized).toBe(true);
      expect(state.error).toBeNull();
      expect(authApi.login).toHaveBeenCalledWith({
        email: 'admin@example.com',
        password: 'password123',
      });
    });

    it('sets error and isLoading=false on failure without clearing existing user', async () => {
      vi.mocked(authApi.login).mockRejectedValueOnce(new Error('Invalid email or password'));

      await useAuthStore.getState().login({ email: 'bad@example.com', password: 'wrong' });

      const state = useAuthStore.getState();
      expect(state.isLoading).toBe(false);
      expect(state.error).toBe('Invalid email or password');
    });
  });

  describe('fetchUser', () => {
    it('sets user, isLoading=false, isInitialized=true, error=null on success', async () => {
      const mockUser = {
        id: 'usr-999',
        email: 'test@example.com',
        display_name: 'Tester',
      };
      vi.mocked(authApi.getMe).mockResolvedValueOnce(mockUser);

      await useAuthStore.getState().fetchUser();

      const state = useAuthStore.getState();
      expect(state.user).toEqual(mockUser);
      expect(state.isLoading).toBe(false);
      expect(state.isInitialized).toBe(true);
      expect(state.error).toBeNull();
    });

    it('sets user=null, isLoading=false, isInitialized=true, error message on failure', async () => {
      vi.mocked(authApi.getMe).mockRejectedValueOnce(new Error('Network error or 401'));

      await useAuthStore.getState().fetchUser();

      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
      expect(state.isLoading).toBe(false);
      expect(state.isInitialized).toBe(true);
      expect(state.error).toBe('Network error or 401');
    });
  });

  describe('logout', () => {
    it('calls authApi.logout and resets user, isLoading=false, isInitialized=true', async () => {
      useAuthStore.setState({
        user: { id: 'usr-123', display_name: 'Active User' },
        isLoading: false,
        isInitialized: true,
        error: null,
      });

      vi.mocked(authApi.logout).mockResolvedValueOnce({ status: 'ok' });

      await useAuthStore.getState().logout();

      expect(authApi.logout).toHaveBeenCalledTimes(1);
      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
      expect(state.isLoading).toBe(false);
      expect(state.isInitialized).toBe(true);
      expect(state.error).toBeNull();
    });

    it('resets user and sets isInitialized=true even if authApi.logout fails', async () => {
      useAuthStore.setState({
        user: { id: 'usr-123' },
        isLoading: false,
        isInitialized: true,
      });

      vi.mocked(authApi.logout).mockRejectedValueOnce(new Error('Logout failed on server'));

      await expect(useAuthStore.getState().logout()).rejects.toThrow('Logout failed on server');

      const state = useAuthStore.getState();
      expect(state.user).toBeNull();
      expect(state.isLoading).toBe(false);
      expect(state.isInitialized).toBe(true);
      expect(state.error).toBeNull();
    });
  });

  describe('setUser', () => {
    it('updates user state directly', () => {
      const user = { id: 'custom-id', display_name: 'Custom' };
      useAuthStore.getState().setUser(user);
      expect(useAuthStore.getState().user).toEqual(user);
    });
  });
});
