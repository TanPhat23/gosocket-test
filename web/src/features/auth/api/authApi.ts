import { axiosClient } from '@/lib/axios';
import { UserSchema, UserDTO } from '../schemas/authSchemas';

export const authApi = {
  async getMe(): Promise<UserDTO> {
    const res = await axiosClient.get<unknown>('/auth/me');
    return UserSchema.parse(res.data);
  },

  async logout(): Promise<{ status: string }> {
    const res = await axiosClient.post<{ status: string }>('/auth/logout');
    return res.data;
  },
};
