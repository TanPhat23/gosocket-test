import { z } from 'zod';

export const CredentialProviderSchema = z.enum(['github', 'jira', 'trello', 'oidc', 'generic']);
export type CredentialProvider = z.infer<typeof CredentialProviderSchema>;

export const CreateCredentialRequestSchema = z.object({
  provider: CredentialProviderSchema,
  label: z.string().min(1, 'Label is required').max(100, 'Label cannot exceed 100 characters'),
  secret: z.string().min(1, 'Secret is required'),
});

export type CreateCredentialInput = z.input<typeof CreateCredentialRequestSchema>;
export type CreateCredentialRequest = CreateCredentialInput;

export const CredentialResponseSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  team_id: z.string().uuid().or(z.string().min(1)),
  provider: CredentialProviderSchema,
  label: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

export type CredentialDTO = z.infer<typeof CredentialResponseSchema>;
export type CredentialResponse = CredentialDTO;
