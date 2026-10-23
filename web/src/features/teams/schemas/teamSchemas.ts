import { z } from 'zod';

export const TeamSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  name: z.string().min(1),
  slug: z.string().optional(),
  role: z.number().int().optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
});

export type TeamDTO = z.infer<typeof TeamSchema>;
export type Team = TeamDTO;

export const ProjectSchema = z.object({
  id: z.string().uuid().or(z.string().min(1)),
  team_id: z.string().uuid().or(z.string().min(1)),
  name: z.string().min(1),
  slug: z.string().optional(),
  repo_url: z.string().url().optional(),
  default_branch: z.string().optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
});

export type ProjectDTO = z.infer<typeof ProjectSchema>;
export type Project = ProjectDTO;
