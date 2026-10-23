import { describe, it, expect } from 'vitest';
import { cleanUserPrompt } from './promptCleaner';

describe('cleanUserPrompt', () => {
  it('returns clean prompt untouched when no system markers exist', () => {
    const prompt = 'Add hero banner section to UI';
    const result = cleanUserPrompt(prompt);
    expect(result.prompt).toBe('Add hero banner section to UI');
    expect(result.systemContext).toBeUndefined();
  });

  it('separates mcp_instructions and tool schemas from user task prompt (Image 1 case)', () => {
    const complexPrompt = `
Add hero banner section to UI

<mcp_instructions>
<server name="context7">
Use tools from this server through execute under tools["context7"].
</server>
</mcp_instructions>

# Code Mode
tools["vibe-check-mcp"].check_constitution({ sessionId: string })
`;

    const result = cleanUserPrompt(complexPrompt);
    expect(result.prompt).toBe('Add hero banner section to UI');
    expect(result.systemContext).toBeDefined();
    expect(result.systemContext).toContain('<mcp_instructions>');
  });

  it('separates workspace_boundary and repository directives from user task prompt', () => {
    const boundedPrompt = `
<workspace_boundary>
CRITICAL WORKSPACE BOUNDARY & REPOSITORY ISOLATION DIRECTIVE:
You are assigned to work EXCLUSIVELY inside this repository worktree:
TARGET REPOSITORY PATH: /data/worktrees/01a12165
</workspace_boundary>

Improve the current gosocket test
`;

    const result = cleanUserPrompt(boundedPrompt);
    expect(result.prompt).toBe('Improve the current gosocket test');
    expect(result.systemContext).toBeDefined();
    expect(result.systemContext).toContain('<workspace_boundary>');
  });

  it('handles empty or null gracefully', () => {
    expect(cleanUserPrompt('')).toEqual({ prompt: '' });
  });
});
