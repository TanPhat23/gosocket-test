import React, { Suspense } from 'react';

// The Monaco bundle is large, so it is loaded the first time an editor is shown, not with the task view.
const CodeEditorBody = React.lazy(() => import('./CodeEditorBody').then((m) => ({ default: m.CodeEditorBody })));

export interface CodeEditorProps {
  value: string;
  language: string;
}

// CodeEditor shows read-only code with syntax highlighting. It fills its parent, so the parent needs a height.
export const CodeEditor: React.FC<CodeEditorProps> = (props) => (
  <Suspense fallback={<p className="p-4 text-xs text-muted-foreground">Loading editor…</p>}>
    <CodeEditorBody {...props} />
  </Suspense>
);
