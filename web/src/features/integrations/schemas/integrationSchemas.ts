import { z } from 'zod';

export const IntegrationProviderSchema = z.enum(['github', 'jira', 'trello']);
export type IntegrationProvider = z.infer<typeof IntegrationProviderSchema>;

export const IntegrationSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  project_id: z.string().uuid().or(z.string().min(1)),
  credential_id: z.string().uuid().or(z.string().min(1)),
  provider: IntegrationProviderSchema.or(z.string()),
  external_id: z.string(),
  external_url: z.string().optional().default(''),
  enabled: z.boolean(),
  config: z.record(z.string(), z.unknown()).optional().default({}),
  created_at: z.string(),
  updated_at: z.string(),
  last_synced_at: z.string().optional().nullable(),
});

export type IntegrationDTO = z.infer<typeof IntegrationSchema>;
export type Integration = IntegrationDTO;

export const CreateIntegrationRequestSchema = z.object({
  credential_id: z.string().min(1, 'Credential is required'),
  provider: IntegrationProviderSchema.or(z.string()).default('github'),
  repo: z
    .string()
    .min(1, 'Repository is required')
    .regex(/^[^/\s]+\/[^/\s]+$/, 'Repository must be in owner/repo format (e.g. facebook/react)'),
  base_branch: z.string().optional().default('main'),
  auto_pr: z.boolean().optional().default(true),
  webhook_secret: z.string().optional(),
  filter_label: z.string().optional(),
});

export type CreateIntegrationInput = z.input<typeof CreateIntegrationRequestSchema>;
export type CreateIntegrationRequest = CreateIntegrationInput;

export const UpdateIntegrationRequestSchema = z.object({
  credential_id: z.string().optional(),
  enabled: z.boolean().optional(),
  base_branch: z.string().optional(),
  auto_pr: z.boolean().optional(),
  webhook_secret: z.string().optional(),
  filter_label: z.string().optional(),
});

export type UpdateIntegrationInput = z.input<typeof UpdateIntegrationRequestSchema>;
export type UpdateIntegrationRequest = UpdateIntegrationInput;
