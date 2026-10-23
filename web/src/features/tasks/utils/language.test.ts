import { describe, expect, it } from 'vitest';
import { languageFromPath } from './language';

describe('languageFromPath', () => {
  it('maps common extensions to Monaco languages', () => {
    expect(languageFromPath('src/app/page.tsx')).toBe('typescript');
    expect(languageFromPath('cmd/server/main.go')).toBe('go');
    expect(languageFromPath('package.json')).toBe('json');
    expect(languageFromPath('docs/README.MD')).toBe('markdown');
  });

  it('recognizes Dockerfiles by name and falls back to plain text', () => {
    expect(languageFromPath('deploy/Dockerfile')).toBe('dockerfile');
    expect(languageFromPath('LICENSE')).toBe('plaintext');
    expect(languageFromPath('.gitignore')).toBe('plaintext');
    expect(languageFromPath('data.unknownext')).toBe('plaintext');
  });
});
