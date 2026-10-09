import { z } from 'zod';

export const TaskStatusSchema = z.enum([
  'backlog',
  'queued',
  'in_progress',
  'review',
  'done',
  'failed',
  'cancelled',
]);
export type TaskStatus = z.infer<typeof TaskStatusSchema>;

export const AssigneeTypeSchema = z.enum(['agent', 'human']);
export type AssigneeType = z.infer<typeof AssigneeTypeSchema>;

export const CreateTaskRequestSchema = z.object({
  project_id: z.string().optional().default(''),
  parent_task_id: z.string().optional(),
  integration_id: z.string().optional(),
  title: z.string().min(1, 'Title is required').max(255, 'Title too long'),
  prompt: z.string().min(1, 'Prompt is required'),
  assignee_id: z.string().min(1, 'Assignee ID is required'),
  assignee_type: AssigneeTypeSchema.optional().default('agent'),
  priority: z.number().int().min(1).max(100).optional().default(50),
  model: z.string().optional(),
  skills: z.array(z.string()).optional().default([]),
  mcp_servers: z.array(z.string()).optional().default([]),
});

export type CreateTaskInput = z.input<typeof CreateTaskRequestSchema>;
export type CreateTaskRequest = CreateTaskInput;

export const TaskSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  project_id: z.string().uuid().or(z.string().min(1)),
  integration_id: z.string().optional().nullable(),
  parent_task_id: z.string().optional().nullable(),
  title: z.string(),
  prompt: z.string(),
  assignee_id: z.string(),
  assignee_type: AssigneeTypeSchema,
  status: TaskStatusSchema,
  priority: z.number().int(),
  model: z.string().optional(),
  pr_url: z.string().url().optional().nullable(),
  pull_request_url: z.string().optional().nullable(),
  external_id: z.string().optional().nullable(),
  external_url: z.string().optional().nullable(),
  skills: z.array(z.string()).default([]),
  mcp_servers: z.array(z.string()).default([]),
  created_at: z.string(),
  updated_at: z.string(),
  heartbeat_at: z.string().optional().nullable(),
  session_id: z.string().optional().nullable(),
});

export const TaskDTOSchema = TaskSchema;
export type TaskDTO = z.infer<typeof TaskSchema>;
export type Task = TaskDTO;

// --- Task Session Types (ADR-004) ---

export interface SessionTokens {
  input: number;
  output: number;
  reasoning?: number;
  cache_read?: number;
  cache_write?: number;
}

export interface SessionToolCall {
  name: string;
  input?: string | Record<string, unknown>;
  output?: string | Record<string, unknown>;
  status?: string;
  exit_code?: number;
}

export interface SessionMessage {
  id: string;
  type?: string;
  role?: 'user' | 'assistant' | 'system' | string;
  agent?: string;
  model?: string;
  text?: string;
  content?: unknown;
  tool_calls?: SessionToolCall[];
  created_at?: number | string | null;
}

export interface TaskSessionResponse {
  task_id: string;
  session_id: string;
  status?: string;
  model?: string;
  title?: string;
  directory?: string;
  tokens?: SessionTokens;
  cost?: number;
  messages: SessionMessage[];
  created_at?: string | null;
  updated_at?: string | null;
}

