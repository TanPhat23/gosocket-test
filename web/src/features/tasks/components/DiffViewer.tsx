import React, { useMemo, useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { FileCode2, GitPullRequest, RotateCcw, FileText, GitMerge, ExternalLink } from 'lucide-react';
import { cn } from '@/lib/utils';
import { CodeEditor } from './CodeEditor';

export interface DiffViewerProps {
  diff?: string;
  loading?: boolean;
  onApprove?: () => void;
  onReject?: () => void;
  prUrl?: string | null;
  isMerged?: boolean;
}

interface DiffFile {
  oldPath: string;
  newPath: string;
  /** The file's own section of the unified diff, shown as-is in the editor. */
  patch: string;
  additions: number;
  deletions: number;
}

// parseUnifiedDiff splits a unified diff into one entry per file. Each file keeps its raw section so the editor can
// show it with diff highlighting.
function parseUnifiedDiff(rawDiff: string): DiffFile[] {
  if (!rawDiff || rawDiff.trim() === '') {
    return [];
  }
  const sections = rawDiff.split(/^(?=diff --git )/m).filter((section) => section.startsWith('diff --git '));
  return sections.map((section) => {
    const header = section.split('\n', 1)[0];
    const match = header.match(/^diff --git a\/(.+) b\/(.+)$/);
    let additions = 0;
    let deletions = 0;
    for (const line of section.split('\n')) {
      if (line.startsWith('+') && !line.startsWith('+++')) additions++;
      else if (line.startsWith('-') && !line.startsWith('---')) deletions++;
    }
    return {
      oldPath: match?.[1] ?? 'unknown',
      newPath: match?.[2] ?? 'unknown',
      patch: `${section.replace(/\n+$/, '')}\n`,
      additions,
      deletions,
    };
  });
}

export const DiffViewer: React.FC<DiffViewerProps> = ({
  diff = '',
  loading = false,
  onApprove,
  onReject,
  prUrl,
  isMerged = false,
}) => {
  const parsedFiles = useMemo(() => parseUnifiedDiff(diff), [diff]);
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);

  const activeFile = parsedFiles[selectedFileIndex] ?? parsedFiles[0];

  const totalAdditions = useMemo(
    () => parsedFiles.reduce((acc, f) => acc + f.additions, 0),
    [parsedFiles]
  );
  const totalDeletions = useMemo(
    () => parsedFiles.reduce((acc, f) => acc + f.deletions, 0),
    [parsedFiles]
  );

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-full w-full rounded-lg border border-border bg-card p-8 text-center min-h-[350px]">
        <div className="size-6 border-2 border-primary border-t-transparent rounded-full animate-spin mb-3" />
        <p className="text-xs text-muted-foreground">Loading code changes...</p>
      </div>
    );
  }

  if (parsedFiles.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full w-full rounded-lg border border-border bg-card p-8 text-center min-h-[350px]">
        <FileCode2 className="size-10 text-muted-foreground/40 mb-3" />
        <h4 className="text-sm font-medium text-foreground">No Code Changes Recorded</h4>
        <p className="text-xs text-muted-foreground mt-1 max-w-sm">
          No file modifications have been captured yet for this task. Changes will appear here once files are committed or staged in the task worktree.
        </p>
      </div>
    );
  }

  const prRef = prUrl ? prUrl.split('/').slice(-2).join('/') || 'Link' : null;

  return (
    <div className="flex flex-col h-full w-full rounded-lg border border-border bg-card overflow-hidden">
      {/* Top Review Bar */}
      <div className="flex items-center justify-between gap-3 p-3 bg-muted/40 border-b border-border">
        <div className="flex items-center gap-3 min-w-0">
          <FileCode2 className="size-4 text-primary shrink-0" />
          <span className="font-semibold text-xs tracking-tight truncate">Code Review &amp; Pull Request Diff</span>
          <div className="flex items-center gap-1.5 text-xs font-mono shrink-0">
            <span className="text-emerald-600 dark:text-emerald-400 font-medium">+{totalAdditions}</span>
            <span className="text-rose-600 dark:text-rose-400 font-medium">-{totalDeletions}</span>
          </div>
          <Badge variant="outline" className="text-[11px] font-mono shrink-0">
            {parsedFiles.length} file{parsedFiles.length === 1 ? '' : 's'}
          </Badge>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {isMerged && prUrl ? (
            <a
              href={prUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 h-7 px-2.5 text-xs font-mono rounded-md bg-purple-500/15 text-purple-600 dark:text-purple-400 border border-purple-500/30 hover:bg-purple-500/25 transition-colors font-medium"
              title="Pull Request merged into base branch"
            >
              <GitMerge className="size-3" />
              <span>PR Merged ({prRef})</span>
              <ExternalLink className="size-2.5" />
            </a>
          ) : prUrl ? (
            <a
              href={prUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 h-7 px-2.5 text-xs font-mono rounded-md bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25 transition-colors font-medium"
              title="View active pull request on GitHub"
            >
              <GitPullRequest className="size-3" />
              <span>PR Active ({prRef})</span>
              <ExternalLink className="size-2.5" />
            </a>
          ) : (
            <>
              {onReject && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={onReject}
                  className="h-7 text-xs text-rose-600 dark:text-rose-400 hover:text-rose-500 hover:bg-rose-500/10 border-rose-500/20"
                >
                  <RotateCcw className="size-3.5 mr-1" />
                  Reject / Re-prompt
                </Button>
              )}

              {onApprove && (
                <Button
                  size="sm"
                  variant="default"
                  onClick={onApprove}
                  className="h-7 text-xs bg-emerald-600 hover:bg-emerald-500 text-white"
                >
                  <GitPullRequest className="size-3.5 mr-1" />
                  Approve &amp; Create PR
                </Button>
              )}
            </>
          )}
        </div>
      </div>

      {/* Main Diff Content: Sidebar + Editor */}
      <div className="flex flex-1 min-h-[350px] overflow-hidden">
        {/* Left: Changed files */}
        <div className="w-64 border-r border-border bg-muted/20 flex flex-col shrink-0">
          <div className="p-2 border-b border-border/50 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
            Changed Files
          </div>
          <div className="flex-1 overflow-y-auto p-1.5 space-y-1">
            {parsedFiles.map((file, idx) => {
              const isSelected = idx === selectedFileIndex;
              return (
                <button
                  key={file.newPath + idx}
                  onClick={() => setSelectedFileIndex(idx)}
                  className={cn(
                    'w-full text-left px-2.5 py-1.5 rounded text-xs flex items-center justify-between transition-colors',
                    isSelected
                      ? 'bg-primary/10 text-primary font-medium'
                      : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                  )}
                >
                  <div className="flex items-center gap-1.5 truncate pr-2">
                    <FileText className="size-3.5 shrink-0" />
                    <span className="truncate font-mono text-[11px]">{file.newPath}</span>
                  </div>
                  <div className="flex items-center gap-1 text-[10px] font-mono shrink-0">
                    <span className="text-emerald-600 dark:text-emerald-400">+{file.additions}</span>
                    <span className="text-rose-600 dark:text-rose-400">-{file.deletions}</span>
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        {/* Right: the selected file's diff in the editor */}
        <div className="flex-1 flex flex-col overflow-hidden bg-background min-w-0">
          {activeFile ? (
            <>
              <div className="p-2.5 px-4 bg-muted/30 border-b border-border/50 flex items-center justify-between text-xs">
                <span className="font-mono font-medium text-foreground">{activeFile.newPath}</span>
                <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
                  <span className="text-emerald-600 dark:text-emerald-400">+{activeFile.additions}</span>
                  <span className="text-rose-600 dark:text-rose-400">-{activeFile.deletions}</span>
                </div>
              </div>
              <div className="relative flex-1 min-h-[300px]" data-testid="diff-editor">
                <div className="absolute inset-0">
                  <CodeEditor value={activeFile.patch} language="diff" />
                </div>
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center h-full text-xs text-muted-foreground">
              Select a file to inspect differences
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
