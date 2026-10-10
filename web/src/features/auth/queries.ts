import { queryOptions } from '@tanstack/react-query';
import { authApi } from './api/authApi';
import { useAuthStore } from './store/useAuthStore';

export const authQueryOptions = () =>
  queryOptions({
    queryKey: ['auth', 'user'],
    queryFn: async () => {
      try {
        const res = await authApi.getCurrentUser();
        useAuthStore.setState({ user: res.user, isLoading: false, isInitialized: true, error: null });
        return res.user;
      } catch {
        useAuthStore.setState({ user: null, isLoading: false, isInitialized: true, error: null });
        return null;
      }
    },
    staleTime: 1000 * 60,
  });
