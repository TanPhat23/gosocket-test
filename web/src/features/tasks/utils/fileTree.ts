export interface FileTreeNode {
  name: string;
  path: string;
  isDir: boolean;
  children: FileTreeNode[];
}

// buildFileTree turns a flat list of slash-separated file paths into a tree. Folders come before files, and both
// are sorted by name at every level.
export function buildFileTree(paths: string[]): FileTreeNode[] {
  const root: FileTreeNode = { name: '', path: '', isDir: true, children: [] };
  const byPath = new Map<string, FileTreeNode>([['', root]]);

  for (const filePath of [...new Set(paths)].sort()) {
    const parts = filePath.split('/').filter((part) => part !== '');
    let parent = root;
    let accumulated = '';
    parts.forEach((part, index) => {
      accumulated = accumulated ? `${accumulated}/${part}` : part;
      let node = byPath.get(accumulated);
      if (!node) {
        node = { name: part, path: accumulated, isDir: index < parts.length - 1, children: [] };
        byPath.set(accumulated, node);
        parent.children.push(node);
      }
      parent = node;
    });
  }

  const sortChildren = (node: FileTreeNode) => {
    node.children.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    node.children.forEach(sortChildren);
  };
  sortChildren(root);
  return root.children;
}
