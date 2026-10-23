const byExtension: Record<string, string> = {
  ts: 'typescript',
  tsx: 'typescript',
  mts: 'typescript',
  cts: 'typescript',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  json: 'json',
  md: 'markdown',
  markdown: 'markdown',
  go: 'go',
  css: 'css',
  scss: 'scss',
  html: 'html',
  yml: 'yaml',
  yaml: 'yaml',
  sh: 'shell',
  bash: 'shell',
  py: 'python',
  rs: 'rust',
  sql: 'sql',
  toml: 'ini',
  ini: 'ini',
  xml: 'xml',
  java: 'java',
  c: 'c',
  h: 'c',
  cpp: 'cpp',
  cc: 'cpp',
  hpp: 'cpp',
  diff: 'diff',
  patch: 'diff',
};

// languageFromPath picks a Monaco language from a file name. Unknown files are shown as plain text.
export function languageFromPath(path: string): string {
  const name = path.split('/').pop() ?? path;
  if (name === 'Dockerfile' || name.startsWith('Dockerfile.')) return 'dockerfile';
  const dot = name.lastIndexOf('.');
  if (dot <= 0) return 'plaintext';
  return byExtension[name.slice(dot + 1).toLowerCase()] ?? 'plaintext';
}
