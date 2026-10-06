import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DiffViewer } from './DiffViewer';

describe('DiffViewer', () => {
  const sampleDiff = `diff --git a/src/hello.ts b/src/hello.ts
--- a/src/hello.ts
+++ b/src/hello.ts
@@ -1,3 +1,3 @@
-console.log("old");
+console.log("new");
`;

  it('renders diff with file names, additions, and deletions', () => {
    render(<DiffViewer diff={sampleDiff} />);

    // File name
    expect(screen.getAllByText('src/hello.ts').length).toBeGreaterThanOrEqual(1);

    // Deletion line content
    expect(screen.getByText('console.log("old");')).toBeInTheDocument();

    // Addition line content
    expect(screen.getByText('console.log("new");')).toBeInTheDocument();
  });

  it('renders review action buttons', () => {
    render(<DiffViewer diff={sampleDiff} />);

    expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /reject/i })).toBeInTheDocument();
  });
});
