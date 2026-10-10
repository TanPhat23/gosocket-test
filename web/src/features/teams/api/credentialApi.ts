import { axiosClient } from '@/lib/axios';
import {
  CredentialResponseSchema,
  CredentialDTO,
  CreateCredentialInput,
  GitHubRepoSchema,
  GitHubRepoDTO,
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

  async updateCredential(
    teamId: string,
    credentialId: string,
    req: { label?: string; secret?: string }
  ): Promise<CredentialDTO> {
    const res = await axiosClient.put<unknown>(
      `/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}`,
      req
    );
    const data = res.data;
    const raw =
      data && typeof data === 'object' && 'credential' in data
        ? (data as { credential: unknown }).credential
        : data;
    return CredentialResponseSchema.parse(raw);
  },

  async listGitHubRepositories(teamId: string, credentialId: string): Promise<GitHubRepoDTO[]> {
    const res = await axiosClient.get<unknown>(
      `/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}/github/repos`
    );
    const data = res.data;
    const rawList = Array.isArray(data)
      ? data
      : (data && typeof data === 'object' && 'repositories' in data && Array.isArray((data as { repositories: unknown[] }).repositories))
      ? (data as { repositories: unknown[] }).repositories
      : [];
    return rawList.map((item) => GitHubRepoSchema.parse(item));
  },

  async listGitHubBranches(teamId: string, credentialId: string, repo: string): Promise<string[]> {
    const res = await axiosClient.get<unknown>(
      `/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}/github/branches`,
      { params: { repo } }
    );
    const data = res.data;
    const rawList = Array.isArray(data)
      ? data
      : data && typeof data === 'object' && 'branches' in data && Array.isArray((data as { branches: unknown[] }).branches)
      ? (data as { branches: unknown[] }).branches
      : [];
    return rawList.map(String);
  },
};
