import { axiosClient } from '@/lib/axios';
import {
  CredentialResponseSchema,
  CredentialDTO,
  CreateCredentialInput,
} from '../schemas/credentialSchemas';

export const credentialApi = {
  async listCredentials(teamId: string): Promise<CredentialDTO[]> {
    const res = await axiosClient.get<unknown>(`/teams/${encodeURIComponent(teamId)}/credentials`);
    const data = res.data;
    const rawList = Array.isArray(data)
      ? data
      : (data && typeof data === 'object' && 'credentials' in data && Array.isArray((data as { credentials: unknown[] }).credentials))
      ? (data as { credentials: unknown[] }).credentials
      : [];
    return rawList.map((item) => CredentialResponseSchema.parse(item));
  },

  async createCredential(teamId: string, req: CreateCredentialInput): Promise<CredentialDTO> {
    const res = await axiosClient.post<unknown>(`/teams/${encodeURIComponent(teamId)}/credentials`, req);
    const data = res.data;
    const raw = (data && typeof data === 'object' && 'credential' in data)
      ? (data as { credential: unknown }).credential
      : data;
    return CredentialResponseSchema.parse(raw);
  },

  async deleteCredential(teamId: string, credentialId: string): Promise<void> {
    await axiosClient.delete<void>(`/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}`);
  },
};
