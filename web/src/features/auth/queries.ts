import { queryOptions } from '@tanstack/react-query';
import { authApi } from './api/authApi';

export const authQueryOptions = () =>
  queryOptions({
    queryKey: ['auth', 'user'],
    queryFn: async () => {
      try {
        const res = await authApi.getCurrentUser();
        return res.user;
      } catch {
        return null;
      }
    },
    staleTime: 1000 * 60,
  });
