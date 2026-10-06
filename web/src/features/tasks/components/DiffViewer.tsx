import React, { useMemo, useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { FileCode2, GitPullRequest, RotateCcw, FileText } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface DiffViewerProps {
  diff?: string;
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

const SAMPLE_DIFF = `diff --git a/internal/agent/executor.go b/internal/agent/executor.go
--- a/internal/agent/executor.go
+++ b/internal/agent/executor.go
@@ -12,6 +12,8 @@ type TaskExecutor struct {
 	worktreeDir string
 	containerId string
+	skillEngine *skills.Engine
+	mcpClient   *mcp.Client
 }
 
 func (e *TaskExecutor) Execute(ctx context.Context) error {
@@ -35,3 +37,4 @@ func (e *TaskExecutor) Execute(ctx context.Context) error {
-	return nil
+	log.Printf("Executing task within isolated worktree: %s", e.worktreeDir)
+	return e.runPipeline(ctx)
 }
diff --git a/internal/agent/skills/engine.go b/internal/agent/skills/engine.go
--- a/internal/agent/skills/engine.go
+++ b/internal/agent/skills/engine.go
@@ -0,0 +1,15 @@
+package skills
+
+type Engine struct {
+	registry map[string]Skill
+}
+
+func NewEngine() *Engine {
+	return &Engine{
+		registry: make(map[string]Skill),
+	}
+}
`;

function parseUnifiedDiff(rawDiff: string): DiffFile[] {
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
      // Chunk header e.g. @@ -12,6 +12,8 @@
      const chunkMatch = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (chunkMatch) {
        oldLine = parseInt(chunkMatch[1], 10);
        newLine = parseInt(chunkMatch[2], 10);
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
  diff = SAMPLE_DIFF,
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
          <Button
            size="sm"
            variant="outline"
            onClick={onReject}
            className="text-xs text-rose-600 dark:text-rose-400 hover:text-rose-500 hover:bg-rose-500/10 border-rose-500/20"
          >
            <RotateCcw className="size-3.5 mr-1" />
            Reject / Re-prompt
          </Button>

          <Button
            size="sm"
            variant="default"
            onClick={onApprove}
            className="text-xs bg-emerald-600 hover:bg-emerald-500 text-white"
          >
            <GitPullRequest className="size-3.5 mr-1" />
            Approve &amp; Create PR
          </Button>
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
                    'w-full text-left px-2 py-1.5 rounded-md text-xs font-mono flex items-center justify-between transition-colors',
                    isSelected
                      ? 'bg-primary/10 text-primary font-semibold'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  )}
                >
                  <span className="truncate pr-2 flex items-center gap-1.5">
                    <FileText className="size-3.5 shrink-0" />
                    {file.newPath}
                  </span>
                  <div className="flex items-center gap-1 text-[10px] shrink-0 font-medium">
                    <span className="text-emerald-600 dark:text-emerald-400">+{file.additions}</span>
                    <span className="text-rose-600 dark:text-rose-400">-{file.deletions}</span>
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        {/* Right: Code Viewer */}
        <div className="flex-1 flex flex-col bg-background overflow-hidden">
          {activeFile ? (
            <div className="flex flex-col h-full">
              {/* File header */}
              <div className="px-4 py-2 border-b border-border/60 bg-muted/30 font-mono text-xs text-foreground flex items-center justify-between">
                <span>{activeFile.newPath}</span>
                <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                  <span className="text-emerald-600 dark:text-emerald-400">+{activeFile.additions}</span>
                  <span className="text-rose-600 dark:text-rose-400">-{activeFile.deletions}</span>
                </div>
              </div>

              {/* Code lines */}
              <div className="flex-1 overflow-auto font-mono text-xs">
                <table className="w-full border-collapse">
                  <tbody>
                    {activeFile.lines.map((line, lineIdx) => {
                      if (line.type === 'header') {
                        return (
                          <tr
                            key={lineIdx}
                            className="bg-blue-500/10 text-blue-500 select-none text-[11px] font-semibold"
                          >
                            <td colSpan={3} className="px-4 py-1">
                              {line.content}
                            </td>
                          </tr>
                        );
                      }

                      const isAddition = line.type === 'addition';
                      const isDeletion = line.type === 'deletion';

                      return (
                        <tr
                          key={lineIdx}
                          className={cn(
                            'hover:bg-muted/40 transition-colors',
                            isAddition && 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-l-2 border-emerald-500',
                            isDeletion && 'bg-rose-500/10 text-rose-600 dark:text-rose-400 border-l-2 border-rose-500'
                          )}
                        >
                          {/* Old line number */}
                          <td className="w-10 px-2 py-0.5 text-right select-none text-muted-foreground/60 text-[11px] border-r border-border/30">
                            {line.oldLineNumber ?? ''}
                          </td>
                          {/* New line number */}
                          <td className="w-10 px-2 py-0.5 text-right select-none text-muted-foreground/60 text-[11px] border-r border-border/30">
                            {line.newLineNumber ?? ''}
                          </td>
                          {/* Prefix sign */}
                          <td className="px-3 py-0.5 whitespace-pre font-mono">
                            <span className="inline-block w-4 select-none opacity-70">
                              {isAddition ? '+' : isDeletion ? '-' : ' '}
                            </span>
                            {line.content}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="flex-1 flex items-center justify-center text-muted-foreground text-xs">
              No file selected
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
