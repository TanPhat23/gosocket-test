import { axiosClient } from '@/lib/axios';
import {
  CredentialResponseSchema,
  CredentialDTO,
  CreateCredentialInput,
} from '../schemas/credentialSchemas';

export const credentialApi = {
  async listCredentials(teamId: string): Promise<CredentialDTO[]> {
    const res = await axiosClient.get<unknown[]>(`/teams/${encodeURIComponent(teamId)}/credentials`);
    return res.data.map((item) => CredentialResponseSchema.parse(item));
  },

  async createCredential(teamId: string, req: CreateCredentialInput): Promise<CredentialDTO> {
    const res = await axiosClient.post<unknown>(`/teams/${encodeURIComponent(teamId)}/credentials`, req);
    return CredentialResponseSchema.parse(res.data);
  },

  async deleteCredential(teamId: string, credentialId: string): Promise<void> {
    await axiosClient.delete<void>(`/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}`);
  },
};
