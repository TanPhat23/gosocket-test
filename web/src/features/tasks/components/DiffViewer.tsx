import React, { useMemo, useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { FileCode2, GitPullRequest, RotateCcw, FileText } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface DiffViewerProps {
  diff?: string;
  loading?: boolean;
  onApprove?: () => void;
  onReject?: () => void;
}

interface DiffLine {
  type: 'addition' | 'deletion' | 'header' | 'context';
  content: string;
  oldLineNumber?: number;
  newLineNumber?: number;
}

interface DiffFile {
  oldPath: string;
  newPath: string;
  lines: DiffLine[];
  additions: number;
  deletions: number;
}

function parseUnifiedDiff(rawDiff: string): DiffFile[] {
  if (!rawDiff || rawDiff.trim() === '') {
    return [];
  }
  const files: DiffFile[] = [];
  const lines = rawDiff.split('\n');

  let currentFile: DiffFile | null = null;
  let oldLine = 0;
  let newLine = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    if (line.startsWith('diff --git')) {
      if (currentFile) {
        files.push(currentFile);
      }
      const match = line.match(/diff --git a\/(.+) b\/(.+)/);
      currentFile = {
        oldPath: match ? match[1] : 'unknown',
        newPath: match ? match[2] : 'unknown',
        lines: [],
        additions: 0,
        deletions: 0,
      };
      continue;
    }

    if (!currentFile) {
      if (line.startsWith('---') || line.startsWith('+++')) {
        continue;
      }
    }

    if (line.startsWith('--- ') || line.startsWith('+++ ')) {
      continue;
    }

    if (line.startsWith('@@')) {
      const match = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (match) {
        oldLine = parseInt(match[1], 10);
        newLine = parseInt(match[2], 10);
      }
      if (currentFile) {
        currentFile.lines.push({
          type: 'header',
          content: line,
        });
      }
      continue;
    }

    if (!currentFile) continue;

    if (line.startsWith('+')) {
      currentFile.additions++;
      currentFile.lines.push({
        type: 'addition',
        content: line.slice(1),
        newLineNumber: newLine++,
      });
    } else if (line.startsWith('-')) {
      currentFile.deletions++;
      currentFile.lines.push({
        type: 'deletion',
        content: line.slice(1),
        oldLineNumber: oldLine++,
      });
    } else {
      currentFile.lines.push({
        type: 'context',
        content: line.startsWith(' ') ? line.slice(1) : line,
        oldLineNumber: oldLine++,
        newLineNumber: newLine++,
      });
    }
  }

  if (currentFile) {
    files.push(currentFile);
  }

  return files;
}

export const DiffViewer: React.FC<DiffViewerProps> = ({
  diff = '',
  loading = false,
  onApprove,
  onReject,
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

  return (
    <div className="flex flex-col h-full w-full rounded-lg border border-border bg-card overflow-hidden">
      {/* Top Review Bar */}
      <div className="flex flex-wrap items-center justify-between gap-2 p-3 bg-muted/40 border-b border-border">
        <div className="flex items-center gap-3">
          <FileCode2 className="size-4 text-primary" />
          <span className="font-semibold text-xs tracking-tight">Code Review &amp; Pull Request Diff</span>
          <div className="flex items-center gap-1.5 text-xs font-mono">
            <span className="text-emerald-600 dark:text-emerald-400 font-medium">+{totalAdditions}</span>
            <span className="text-rose-600 dark:text-rose-400 font-medium">-{totalDeletions}</span>
          </div>
          <Badge variant="outline" className="text-[11px] font-mono">
            {parsedFiles.length} file{parsedFiles.length === 1 ? '' : 's'}
          </Badge>
        </div>

        <div className="flex items-center gap-2">
          {onReject && (
            <Button
              size="sm"
              variant="outline"
              onClick={onReject}
              className="text-xs text-rose-600 dark:text-rose-400 hover:text-rose-500 hover:bg-rose-500/10 border-rose-500/20"
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
              className="text-xs bg-emerald-600 hover:bg-emerald-500 text-white"
            >
              <GitPullRequest className="size-3.5 mr-1" />
              Approve &amp; Create PR
            </Button>
          )}
        </div>
      </div>

      {/* Main Diff Content: Sidebar + Code View */}
      <div className="flex flex-1 min-h-[350px] overflow-hidden">
        {/* Left: Files Tree/List */}
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

        {/* Right: File Diff Viewer */}
        <div className="flex-1 flex flex-col overflow-hidden bg-background">
          {activeFile ? (
            <>
              {/* File Header */}
              <div className="p-2.5 px-4 bg-muted/30 border-b border-border/50 flex items-center justify-between text-xs">
                <span className="font-mono font-medium text-foreground">{activeFile.newPath}</span>
                <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
                  <span className="text-emerald-600 dark:text-emerald-400">+{activeFile.additions}</span>
                  <span className="text-rose-600 dark:text-rose-400">-{activeFile.deletions}</span>
                </div>
              </div>

              {/* Lines */}
              <div className="flex-1 overflow-auto font-mono text-xs select-text">
                <table className="w-full border-collapse">
                  <tbody>
                    {activeFile.lines.map((line, lIdx) => {
                      if (line.type === 'header') {
                        return (
                          <tr key={lIdx} className="bg-primary/5 text-primary/80 font-medium">
                            <td colSpan={3} className="py-1 px-4 text-[11px]">
                              {line.content}
                            </td>
                          </tr>
                        );
                      }

                      return (
                        <tr
                          key={lIdx}
                          className={cn(
                            'hover:bg-muted/30 transition-colors',
                            line.type === 'addition' && 'bg-emerald-500/10 text-emerald-950 dark:text-emerald-200',
                            line.type === 'deletion' && 'bg-rose-500/10 text-rose-950 dark:text-rose-200'
                          )}
                        >
                          <td className="w-12 text-right pr-2 select-none text-[10px] text-muted-foreground/60 border-r border-border/30">
                            {line.oldLineNumber ?? ''}
                          </td>
                          <td className="w-12 text-right pr-2 select-none text-[10px] text-muted-foreground/60 border-r border-border/30">
                            {line.newLineNumber ?? ''}
                          </td>
                          <td className="px-3 whitespace-pre overflow-x-visible">
                            <span
                              className={cn(
                                'inline-block w-4 select-none',
                                line.type === 'addition' && 'text-emerald-600 dark:text-emerald-400 font-bold',
                                line.type === 'deletion' && 'text-rose-600 dark:text-rose-400 font-bold',
                                line.type === 'context' && 'text-transparent'
                              )}
                            >
                              {line.type === 'addition' ? '+' : line.type === 'deletion' ? '-' : ' '}
                            </span>
                            {line.content}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
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
