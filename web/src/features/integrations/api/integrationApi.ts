import { axiosClient } from '@/lib/axios';
import {
  IntegrationSchema,
  IntegrationDTO,
  CreateIntegrationInput,
  UpdateIntegrationInput,
} from '../schemas/integrationSchemas';

export const integrationApi = {
  async listProjectIntegrations(projectId: string): Promise<IntegrationDTO[]> {
    const res = await axiosClient.get<unknown>(
      `/projects/${encodeURIComponent(projectId)}/integrations`
    );
    const data = res.data;
    const rawList = Array.isArray(data)
      ? data
      : (data && typeof data === 'object' && 'integrations' in data && Array.isArray((data as { integrations: unknown[] }).integrations))
      ? (data as { integrations: unknown[] }).integrations
      : [];
    return rawList.map((item) => IntegrationSchema.parse(item));
  },

  async createProjectIntegration(
    projectId: string,
    req: CreateIntegrationInput
  ): Promise<IntegrationDTO> {
    const res = await axiosClient.post<unknown>(
      `/projects/${encodeURIComponent(projectId)}/integrations`,
      req
    );
    const data = res.data;
    const raw = (data && typeof data === 'object' && 'integration' in data)
      ? (data as { integration: unknown }).integration
      : data;
    return IntegrationSchema.parse(raw);
  },

  async updateProjectIntegration(
    projectId: string,
    integrationId: string,
    req: UpdateIntegrationInput
  ): Promise<IntegrationDTO> {
    const res = await axiosClient.put<unknown>(
      `/projects/${encodeURIComponent(projectId)}/integrations/${encodeURIComponent(integrationId)}`,
      req
    );
    const data = res.data;
    const raw = (data && typeof data === 'object' && 'integration' in data)
      ? (data as { integration: unknown }).integration
      : data;
    return IntegrationSchema.parse(raw);
  },

  async deleteProjectIntegration(
    projectId: string,
    integrationId: string
  ): Promise<void> {
    await axiosClient.delete<void>(
      `/projects/${encodeURIComponent(projectId)}/integrations/${encodeURIComponent(integrationId)}`
    );
  },
};
