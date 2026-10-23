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

  it('renders diff with file names, additions, and deletions', async () => {
    render(<DiffViewer diff={sampleDiff} />);

    // File name
    expect(screen.getAllByText('src/hello.ts').length).toBeGreaterThanOrEqual(1);

    // The selected file's patch is shown in the editor, with its deletion and addition lines.
    const editor = await screen.findByTestId('monaco-editor');
    expect(editor).toHaveTextContent('-console.log("old");');
    expect(editor).toHaveTextContent('+console.log("new");');
  });

  it('renders review action buttons when handlers provided', () => {
    render(<DiffViewer diff={sampleDiff} onApprove={() => {}} onReject={() => {}} />);

    expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /reject/i })).toBeInTheDocument();
  });

  it('renders PR Merged badge and hides approve/reject buttons when isMerged is true', () => {
    render(
      <DiffViewer
        diff={sampleDiff}
        prUrl="https://github.com/org/repo/pull/1"
        isMerged={true}
        onApprove={() => {}}
        onReject={() => {}}
      />
    );

    expect(screen.getByText(/PR Merged/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /approve/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reject/i })).not.toBeInTheDocument();
  });

  it('renders PR Active badge and hides approve/reject buttons when prUrl is active and not merged', () => {
    render(
      <DiffViewer
        diff={sampleDiff}
        prUrl="https://github.com/org/repo/pull/1"
        isMerged={false}
        onApprove={() => {}}
        onReject={() => {}}
      />
    );

    expect(screen.getByText(/PR Active/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /approve/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reject/i })).not.toBeInTheDocument();
  });

  it('renders clean empty state when diff is empty', () => {
    render(<DiffViewer diff="" />);
    expect(screen.getByText('No Code Changes Recorded')).toBeInTheDocument();
  });
});
