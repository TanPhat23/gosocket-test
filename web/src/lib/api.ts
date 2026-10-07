import { RawAxiosRequestHeaders } from 'axios';
import { axiosClient, ApiError } from './axios';
import { authApi } from '@/features/auth/api/authApi';
import { taskApi } from '@/features/tasks/api/taskApi';
import { teamApi } from '@/features/teams/api/teamApi';
import { credentialApi } from '@/features/teams/api/credentialApi';
import { integrationApi } from '@/features/integrations/api/integrationApi';
import type {
  UserDTO as User,
  CredentialDTO as CredentialResponse,
  CreateCredentialInput as CreateCredentialRequest,
  TeamDTO as Team,
  ProjectDTO as Project,
  TaskDTO as Task,
  CreateTaskInput as CreateTaskRequest,
  TaskStatus,
  AssigneeType,
  CredentialProvider,
  IntegrationDTO as Integration,
  CreateIntegrationInput as CreateIntegrationRequest,
  UpdateIntegrationInput as UpdateIntegrationRequest,
  TaskSessionResponse,
  SessionTokens,
  SessionMessage,
  SessionToolCall,
} from './schemas';

export type {
  User,
  CredentialResponse,
  CreateCredentialRequest,
  Team,
  Project,
  Task,
  CreateTaskRequest,
  TaskStatus,
  AssigneeType,
  CredentialProvider,
  Integration,
  CreateIntegrationRequest,
  UpdateIntegrationRequest,
  TaskSessionResponse,
  SessionTokens,
  SessionMessage,
  SessionToolCall,
};

export interface WebhookResult {
  status: 'processed' | 'duplicate' | 'ignored';
  delivery_id?: string;
  event_id?: string;
  task_id?: string;
  reason?: string;
}

export { ApiError };

export class ApiClient {
  public readonly client = axiosClient;

  // Auth endpoints
  getMe = authApi.getMe;
  logout = authApi.logout;

  // Teams & Projects
  getTeams = teamApi.getTeams;
  getProjects = teamApi.getProjects;

  // Team Credentials
  listCredentials = credentialApi.listCredentials;
  createCredential = credentialApi.createCredential;
  deleteCredential = credentialApi.deleteCredential;

  // Tasks
  getTasks = taskApi.getTasks;
  getTask = taskApi.getTask;
  createTask = taskApi.createTask;
  cancelTask = taskApi.cancelTask;
  createTaskPR = taskApi.createTaskPR;
  getTaskSession = taskApi.getTaskSession;
  promptTaskSession = taskApi.promptTaskSession;
  compactTaskSession = taskApi.compactTaskSession;

  // Integrations
  listProjectIntegrations = integrationApi.listProjectIntegrations;
  createProjectIntegration = integrationApi.createProjectIntegration;
  updateProjectIntegration = integrationApi.updateProjectIntegration;
  deleteProjectIntegration = integrationApi.deleteProjectIntegration;

  // Webhooks
  async sendWebhook(provider: string, payload: unknown, headers?: RawAxiosRequestHeaders): Promise<WebhookResult> {
    const res = await this.client.post<WebhookResult>(`/webhooks/${encodeURIComponent(provider)}`, payload, {
      headers,
    });
    return res.data;
  }
}

export const apiClient = new ApiClient();
