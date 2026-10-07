import { axiosClient } from '@/lib/axios';
import {
  IntegrationSchema,
  IntegrationDTO,
  CreateIntegrationInput,
  UpdateIntegrationInput,
} from '../schemas/integrationSchemas';

export const integrationApi = {
  async listProjectIntegrations(projectId: string): Promise<IntegrationDTO[]> {
    const res = await axiosClient.get<unknown[]>(
      `/projects/${encodeURIComponent(projectId)}/integrations`
    );
    return res.data.map((item) => IntegrationSchema.parse(item));
  },

  async createProjectIntegration(
    projectId: string,
    req: CreateIntegrationInput
  ): Promise<IntegrationDTO> {
    const res = await axiosClient.post<unknown>(
      `/projects/${encodeURIComponent(projectId)}/integrations`,
      req
    );
    return IntegrationSchema.parse(res.data);
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
    return IntegrationSchema.parse(res.data);
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
