import { describe, expect, it } from 'vitest';
import { buildFileTree } from './fileTree';

describe('buildFileTree', () => {
  it('nests files under their folders', () => {
    const tree = buildFileTree(['src/app/page.tsx', 'src/app/layout.tsx', 'README.md']);
    expect(tree.map((n) => n.name)).toEqual(['src', 'README.md']);
    const src = tree[0];
    expect(src.isDir).toBe(true);
    expect(src.children[0].name).toBe('app');
    expect(src.children[0].children.map((n) => n.path)).toEqual([
      'src/app/layout.tsx',
      'src/app/page.tsx',
    ]);
  });

  it('lists folders before files and sorts by name at each level', () => {
    const tree = buildFileTree(['zeta.ts', 'alpha/b.ts', 'beta.ts', 'alpha/a.ts']);
    expect(tree.map((n) => n.name)).toEqual(['alpha', 'beta.ts', 'zeta.ts']);
    expect(tree[0].children.map((n) => n.name)).toEqual(['a.ts', 'b.ts']);
  });

  it('keeps each path once and ignores empty segments', () => {
    const tree = buildFileTree(['a//b.ts', 'a/b.ts', '/a/b.ts']);
    expect(tree).toHaveLength(1);
    expect(tree[0].name).toBe('a');
    expect(tree[0].children.map((n) => n.path)).toEqual(['a/b.ts']);
  });

  it('returns an empty tree for no files', () => {
    expect(buildFileTree([])).toEqual([]);
  });
});
