/**
 * Separates user task prompt from injected system preambles, MCP tool catalogs,
 * and environment context injected into the initial OpenCode session turn.
 */
export function cleanUserPrompt(rawText: string): { prompt: string; systemContext?: string } {
  if (!rawText) return { prompt: '' };

  const text = rawText.trim();

  // Common system preamble markers injected by OpenCode or harnesses
  const systemMarkers = [
    '<mcp_instructions>',
    '<available_skills>',
    '<env>',
    '<EXTREMELY_IMPORTANT>',
    '<priority>',
    '# Your Model',
    '## Available tools',
    '# Code Mode',
    'Platform Adaptation',
    'Tool Mapping for OpenCode',
    'You are Antigravity',
    'You are the engineering coordinator',
    '@integer @minimum',
  ];

  // Check if text contains any system marker
  const hasSystemMarker = systemMarkers.some((marker) => text.includes(marker));
  if (!hasSystemMarker) {
    return { prompt: text };
  }

  // Extract <mcp_instructions> blocks
  const mcpMatches = text.match(/<mcp_instructions>[\s\S]*?<\/mcp_instructions>/gi) || [];
  const skillsMatches = text.match(/<available_skills>[\s\S]*?<\/available_skills>/gi) || [];
  const envMatches = text.match(/<env>[\s\S]*?<\/env>/gi) || [];
  const importantMatches = text.match(/<EXTREMELY_IMPORTANT>[\s\S]*?<\/EXTREMELY_IMPORTANT>/gi) || [];

  const systemPieces: string[] = [
    ...mcpMatches,
    ...skillsMatches,
    ...envMatches,
    ...importantMatches,
  ];

  let cleanedPrompt = text;
  for (const piece of systemPieces) {
    cleanedPrompt = cleanedPrompt.replace(piece, '');
  }

  // Also remove standalone tool schemas or # Code Mode blocks
  cleanedPrompt = cleanedPrompt
    .replace(/# Code Mode[\s\S]*?(?=\n\n[A-Z0-9#]|\n\n$|$)/gi, '')
    .replace(/## Available tools[\s\S]*?(?=\n\n[A-Z0-9#]|\n\n$|$)/gi, '')
    .replace(/# Your Model[\s\S]*?(?=\n\n[A-Z0-9#]|\n\n$|$)/gi, '')
    .replace(/You are the engineering coordinator[\s\S]*?unresolved risks\./gi, '')
    .replace(/You are Antigravity[\s\S]*?(?=\n\n|$)/gi, '')
    .replace(/<priority>[\s\S]*?<\/priority>/gi, '')
    .replace(/Tool Mapping for OpenCode:[\s\S]*?(?=\n\n|$)/gi, '')
    .trim();

  // If after cleaning we still have text, that is the clean user prompt
  if (cleanedPrompt) {
    return {
      prompt: cleanedPrompt,
      systemContext: systemPieces.length > 0 ? systemPieces.join('\n\n') : undefined,
    };
  }

  // Fallback: if everything was stripped, attempt to extract the first readable line or user instruction
  const firstNonSystemLine = text
    .split('\n')
    .map((l) => l.trim())
    .find((l) => l && !systemMarkers.some((m) => l.includes(m)) && !l.startsWith('<') && !l.startsWith('•'));

  return {
    prompt: firstNonSystemLine || 'Task Execution Initialized',
    systemContext: text,
  };
}
