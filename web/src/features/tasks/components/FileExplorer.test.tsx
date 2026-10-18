import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

const getTaskFiles = vi.fn();
const getTaskFile = vi.fn();

vi.mock('../api/taskApi', () => ({
  taskApi: {
    getTaskFiles: (...args: unknown[]) => getTaskFiles(...args),
    getTaskFile: (...args: unknown[]) => getTaskFile(...args),
  },
}));

import { FileExplorer } from './FileExplorer';

describe('FileExplorer', () => {
  beforeEach(() => {
    getTaskFiles.mockReset();
    getTaskFile.mockReset();
  });

  it('loads the files only once the tab is active, then shows a file on click', async () => {
    getTaskFiles.mockResolvedValue({ task_id: 't1', source: 'workspace', files: ['src/main.go', 'README.md'] });
    getTaskFile.mockResolvedValue({
      task_id: 't1', path: 'README.md', source: 'workspace', size: 5, content: 'hello', binary: false, truncated: false,
    });

    const { rerender } = render(<FileExplorer taskId="t1" active={false} />);
    expect(getTaskFiles).not.toHaveBeenCalled();

    rerender(<FileExplorer taskId="t1" active />);
    fireEvent.click(await screen.findByText('README.md'));

    expect(await screen.findByTestId('monaco-editor')).toHaveTextContent('hello');
    expect(getTaskFile).toHaveBeenCalledWith('t1', 'README.md');
  });

  it('reads each file once while it is fresh, however often it is clicked', async () => {
    getTaskFiles.mockResolvedValue({ task_id: 't1', source: 'workspace', files: ['Dockerfile', 'main.go'] });
    getTaskFile.mockResolvedValue({
      task_id: 't1', path: 'Dockerfile', source: 'workspace', size: 3, content: 'FROM', binary: false, truncated: false,
    });

    render(<FileExplorer taskId="t1" active />);
    const entry = await screen.findByText('Dockerfile');
    fireEvent.click(entry);
    await screen.findByTestId('monaco-editor');
    fireEvent.click(screen.getByText('main.go'));
    fireEvent.click(screen.getByText('Dockerfile'));
    fireEvent.click(screen.getByText('main.go'));
    fireEvent.click(screen.getByText('Dockerfile'));

    await screen.findByTestId('monaco-editor');
    expect(getTaskFile.mock.calls.filter((c) => c[1] === 'Dockerfile')).toHaveLength(1);
    expect(getTaskFile.mock.calls.filter((c) => c[1] === 'main.go')).toHaveLength(1);
    expect(getTaskFiles).toHaveBeenCalledTimes(1);
  });

  it('does not show the contents of a binary file', async () => {
    getTaskFiles.mockResolvedValue({ task_id: 't1', source: 'workspace', files: ['logo.png'] });
    getTaskFile.mockResolvedValue({
      task_id: 't1', path: 'logo.png', source: 'workspace', size: 2048, content: '', binary: true, truncated: false,
    });

    render(<FileExplorer taskId="t1" active />);
    fireEvent.click(await screen.findByText('logo.png'));

    expect(await screen.findByText(/Binary file/)).toBeInTheDocument();
    expect(screen.queryByTestId('file-explorer-content')).not.toBeInTheDocument();
  });

  it('explains an unavailable workspace in plain words', async () => {
    getTaskFiles.mockRejectedValue({ response: { status: 503 } });

    render(<FileExplorer taskId="t1" active />);

    expect(await screen.findByText(/workspace is not reachable/)).toBeInTheDocument();
  });
});
