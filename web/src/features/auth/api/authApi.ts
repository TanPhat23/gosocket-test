import { axiosClient } from '@/lib/axios';
import { AuthMeResponseSchema, UserDTO } from '../schemas/authSchemas';

export const authApi = {
  async getMe(): Promise<UserDTO> {
    const res = await axiosClient.get<unknown>('/auth/me');
    return AuthMeResponseSchema.parse(res.data);
  },

  async logout(): Promise<{ status: string }> {
    const res = await axiosClient.post<{ status: string }>('/auth/logout');
    return res.data;
  },
};
