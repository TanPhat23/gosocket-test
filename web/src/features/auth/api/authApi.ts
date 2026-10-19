import { axiosClient } from '@/lib/axios';
import { AuthMeResponseSchema, UserDTO } from '../schemas/authSchemas';

export const authApi = {
  async login(credentials: { email: string; password: string }): Promise<UserDTO> {
    const res = await axiosClient.post<unknown>('/auth/login', credentials);
    return AuthMeResponseSchema.parse(res.data);
  },

  async getMe(): Promise<UserDTO> {
    const res = await axiosClient.get<unknown>('/auth/me');
    return AuthMeResponseSchema.parse(res.data);
  },

  async getCurrentUser(): Promise<{ user: UserDTO }> {
    const user = await this.getMe();
    return { user };
  },

  // Sets the author name and email the user's tasks are committed as. Both empty clears it.
  async setGitIdentity(identity: { git_name: string; git_email: string }): Promise<UserDTO> {
    const res = await axiosClient.put<unknown>('/me/git-identity', identity);
    return AuthMeResponseSchema.parse(res.data);
  },

  async logout(): Promise<{ status: string }> {
    const res = await axiosClient.post<{ status: string }>('/auth/logout');
    return res.data;
  },
};
