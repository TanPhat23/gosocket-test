import { create } from 'zustand';
import { authApi } from '../api/authApi';
import type { UserDTO } from '../schemas/authSchemas';

interface AuthState {
  user: UserDTO | null;
  isLoading: boolean;
  isInitialized: boolean;
  error: string | null;
  fetchUser: () => Promise<void>;
  logout: () => Promise<void>;
  setUser: (user: UserDTO | null) => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isInitialized: false,
  error: null,

  fetchUser: async () => {
    set({ isLoading: true, error: null });
    try {
      const user = await authApi.getMe();
      set({ user, isLoading: false, isInitialized: true, error: null });
    } catch (err) {
      set({
        user: null,
        isLoading: false,
        isInitialized: true,
        error: err instanceof Error ? err.message : 'Failed to fetch user',
      });
    }
  },

  logout: async () => {
    try {
      await authApi.logout();
    } finally {
      set({ user: null, isLoading: false, isInitialized: true, error: null });
    }
  },

  setUser: (user) => set({ user }),
}));

// Fine-grained selectors to avoid re-rendering parent components
export const useUser = () => useAuthStore((state) => state.user);
export const useIsAuthenticated = () => useAuthStore((state) => !!state.user);
export const useAuthLoading = () => useAuthStore((state) => state.isLoading);
export const useAuthInitialized = () => useAuthStore((state) => state.isInitialized);
export const useAuthError = () => useAuthStore((state) => state.error);
