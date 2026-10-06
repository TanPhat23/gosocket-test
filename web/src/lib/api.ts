import { RawAxiosRequestHeaders } from 'axios';
import { axiosClient, ApiError } from './axios';
import { authApi } from '@/features/auth/api/authApi';
import { taskApi } from '@/features/tasks/api/taskApi';
import { teamApi } from '@/features/teams/api/teamApi';
import { credentialApi } from '@/features/teams/api/credentialApi';
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

  // Webhooks
  async sendWebhook(provider: string, payload: unknown, headers?: RawAxiosRequestHeaders): Promise<WebhookResult> {
    const res = await this.client.post<WebhookResult>(`/webhooks/${encodeURIComponent(provider)}`, payload, {
      headers,
    });
    return res.data;
  }
}

export const apiClient = new ApiClient();
