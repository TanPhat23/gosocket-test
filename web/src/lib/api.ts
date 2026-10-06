import axios, { AxiosInstance, AxiosError, RawAxiosRequestHeaders } from 'axios';

export interface User {
  id: string;
  issuer: string;
  subject: string;
  email?: string;
  display_name?: string;
}

export interface LoginResponse {
  status: string;
  user: User;
}

export type CredentialProvider = 'github' | 'jira' | 'trello' | 'oidc' | 'generic';

export interface CreateCredentialRequest {
  provider: CredentialProvider;
  label: string;
  secret: string;
}

export interface CredentialResponse {
  id: string;
  team_id: string;
  provider: CredentialProvider;
  label: string;
  created_at: string;
  updated_at: string;
}

export interface Team {
  id: string;
  name: string;
  slug?: string;
  role?: number;
  created_at?: string;
  updated_at?: string;
}

export interface Project {
  id: string;
  team_id: string;
  name: string;
  slug?: string;
  repo_url?: string;
  default_branch?: string;
  created_at?: string;
  updated_at?: string;
}

export type TaskStatus = 'backlog' | 'queued' | 'in_progress' | 'review' | 'done' | 'failed' | 'cancelled';
export type AssigneeType = 'agent' | 'human';

export interface Task {
  id: string;
  project_id: string;
  title: string;
  prompt: string;
  assignee_id: string;
  assignee_type: AssigneeType;
  status: TaskStatus;
  priority: number;
  model?: string;
  pr_url?: string;
  skills: string[];
  mcp_servers: string[];
  created_at: string;
  updated_at: string;
  heartbeat_at?: string;
}

export interface CreateTaskRequest {
  project_id: string;
  title: string;
  prompt: string;
  assignee_id: string;
  assignee_type?: AssigneeType;
  priority?: number;
  model?: string;
  skills?: string[];
  mcp_servers?: string[];
}

export interface WebhookResult {
  status: 'processed' | 'duplicate' | 'ignored';
  delivery_id?: string;
  event_id?: string;
  task_id?: string;
  reason?: string;
}

export class ApiError extends Error {
  constructor(public status: number, message: string, public body?: unknown) {
    super(message);
    this.name = 'ApiError';
  }
}

export class ApiClient {
  public readonly client: AxiosInstance;

  constructor(baseURL: string = '/api/v1') {
    this.client = axios.create({
      baseURL,
      withCredentials: true,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    this.client.interceptors.response.use(
      (response) => response,
      (error: AxiosError<{ error?: string; message?: string }>) => {
        if (error.response) {
          const status = error.response.status;
          const data = error.response.data;
          const msg = data?.error || data?.message || error.message || `Request failed with status ${status}`;
          return Promise.reject(new ApiError(status, msg, data));
        }
        return Promise.reject(new ApiError(0, error.message || 'Network error'));
      }
    );
  }

  // Auth endpoints
  async getMe(): Promise<User> {
    const res = await this.client.get<User>('/auth/me');
    return res.data;
  }

  async logout(): Promise<{ status: string }> {
    const res = await this.client.post<{ status: string }>('/auth/logout');
    return res.data;
  }

  // Teams & Projects
  async getTeams(): Promise<Team[]> {
    const res = await this.client.get<Team[]>('/teams');
    return res.data;
  }

  async getProjects(teamId?: string): Promise<Project[]> {
    const res = await this.client.get<Project[]>('/projects', {
      params: teamId ? { team_id: teamId } : undefined,
    });
    return res.data;
  }

  // Team Credentials
  async listCredentials(teamId: string): Promise<CredentialResponse[]> {
    const res = await this.client.get<CredentialResponse[]>(`/teams/${encodeURIComponent(teamId)}/credentials`);
    return res.data;
  }

  async createCredential(teamId: string, req: CreateCredentialRequest): Promise<CredentialResponse> {
    const res = await this.client.post<CredentialResponse>(`/teams/${encodeURIComponent(teamId)}/credentials`, req);
    return res.data;
  }

  async deleteCredential(teamId: string, credentialId: string): Promise<void> {
    await this.client.delete<void>(`/teams/${encodeURIComponent(teamId)}/credentials/${encodeURIComponent(credentialId)}`);
  }

  // Tasks
  async getTasks(projectId?: string): Promise<Task[]> {
    const res = await this.client.get<Task[]>('/tasks', {
      params: projectId ? { project_id: projectId } : undefined,
    });
    return res.data;
  }

  async getTask(taskId: string): Promise<Task> {
    const res = await this.client.get<Task>(`/tasks/${encodeURIComponent(taskId)}`);
    return res.data;
  }

  async createTask(req: CreateTaskRequest): Promise<Task> {
    const res = await this.client.post<Task>('/tasks', req);
    return res.data;
  }

  async cancelTask(taskId: string): Promise<{ status: string }> {
    const res = await this.client.post<{ status: string }>(`/tasks/${encodeURIComponent(taskId)}/cancel`);
    return res.data;
  }

  // Webhooks
  async sendWebhook(provider: string, payload: unknown, headers?: RawAxiosRequestHeaders): Promise<WebhookResult> {
    const res = await this.client.post<WebhookResult>(`/webhooks/${encodeURIComponent(provider)}`, payload, {
      headers,
    });
    return res.data;
  }
}

export const apiClient = new ApiClient();
