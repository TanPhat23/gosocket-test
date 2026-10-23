import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, File as FileIcon, Folder, FolderOpen, Search } from 'lucide-react';
import { taskFileQueryOptions, taskFilesQueryOptions } from '../queries';
import { buildFileTree, FileTreeNode } from '../utils/fileTree';
import { languageFromPath } from '../utils/language';
import { CodeEditor } from './CodeEditor';

interface FileExplorerProps {
  taskId: string;
  /** The files are loaded the first time the tab is shown, not when the modal opens. */
  active: boolean;
}

function errorMessage(err: unknown): string {
  const status = (err as { response?: { status?: number } })?.response?.status;
  if (status === 503) return 'Files are unavailable: the project workspace is not reachable right now.';
  if (status === 404) return 'That file is not in this task workspace.';
  return 'Could not load the files for this task.';
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export const FileExplorer: React.FC<FileExplorerProps> = ({ taskId, active }) => {
  const [filter, setFilter] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [expandedFor, setExpandedFor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  // Each query is cached by task (and by path), so reopening a file or switching tabs does not re-read the workspace.
  const filesQuery = useQuery({ ...taskFilesQueryOptions(taskId), enabled: active && !!taskId });
  const fileQuery = useQuery({
    ...taskFileQueryOptions(taskId, selected ?? ''),
    enabled: !!taskId && !!selected,
  });

  const files = useMemo(() => filesQuery.data?.files ?? [], [filesQuery.data]);
  const source = filesQuery.data?.source ?? '';
  const listError = filesQuery.isError ? errorMessage(filesQuery.error) : null;
  const loadingList = filesQuery.isPending && active && !!taskId;
  const file = selected ? fileQuery.data ?? null : null;
  const fileError = selected && fileQuery.isError ? errorMessage(fileQuery.error) : null;
  const loadingFile = !!selected && fileQuery.isPending;

  useEffect(() => {
    setSelected(null);
    setExpandedFor(null);
  }, [taskId]);

  // Top-level folders start open once per task, so the structure is visible at once.
  useEffect(() => {
    if (!filesQuery.data || expandedFor === taskId) return;
    setExpanded(new Set(buildFileTree(filesQuery.data.files).filter((n) => n.isDir).map((n) => n.path)));
    setExpandedFor(taskId);
  }, [filesQuery.data, expandedFor, taskId]);

  const visibleFiles = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return query ? files.filter((p) => p.toLowerCase().includes(query)) : files;
  }, [files, filter]);
  const tree = useMemo(() => buildFileTree(visibleFiles), [visibleFiles]);

  const toggle = (path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  const renderNodes = (nodes: FileTreeNode[], depth: number): React.ReactNode =>
    nodes.map((node) => {
      const indent = { paddingLeft: `${depth * 12 + 8}px` };
      if (node.isDir) {
        const open = filter.trim() !== '' || expanded.has(node.path);
        return (
          <div key={`dir:${node.path}`}>
            <button
              type="button"
              onClick={() => toggle(node.path)}
              className="flex w-full items-center gap-1.5 py-1 pr-2 text-left text-xs text-foreground hover:bg-muted/60"
              style={indent}
              aria-expanded={open}
            >
              {open ? <ChevronDown className="size-3.5 shrink-0" /> : <ChevronRight className="size-3.5 shrink-0" />}
              {open ? <FolderOpen className="size-3.5 shrink-0 text-primary" /> : <Folder className="size-3.5 shrink-0 text-primary" />}
              <span className="truncate">{node.name}</span>
            </button>
            {open && renderNodes(node.children, depth + 1)}
          </div>
        );
      }
      const isSelected = selected === node.path;
      return (
        <button
          key={`file:${node.path}`}
          type="button"
          onClick={() => setSelected(node.path)}
          className={`flex w-full items-center gap-1.5 py-1 pr-2 text-left text-xs hover:bg-muted/60 ${
            isSelected ? 'bg-muted text-foreground' : 'text-muted-foreground'
          }`}
          style={indent}
          data-testid="file-explorer-file"
        >
          <FileIcon className="ml-[18px] size-3.5 shrink-0" />
          <span className="truncate">{node.name}</span>
        </button>
      );
    });

  return (
    <div className="flex h-full min-h-[380px] w-full flex-col overflow-hidden rounded-lg border border-border bg-card md:flex-row">
      <div className="flex min-h-0 w-full flex-col border-b border-border md:w-72 md:border-b-0 md:border-r">
        <div className="flex items-center gap-2 border-b border-border px-2 py-1.5">
          <Search className="size-3.5 text-muted-foreground" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter files"
            aria-label="Filter files"
            className="w-full bg-transparent text-xs text-foreground outline-none placeholder:text-muted-foreground"
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto py-1" data-testid="file-explorer-tree">
          {loadingList && <p className="px-3 py-2 text-xs text-muted-foreground">Loading files…</p>}
          {listError && <p className="px-3 py-2 text-xs text-destructive">{listError}</p>}
          {!loadingList && !listError && files.length === 0 && (
            <p className="px-3 py-2 text-xs text-muted-foreground">No files in this task workspace yet.</p>
          )}
          {!loadingList && !listError && files.length > 0 && visibleFiles.length === 0 && (
            <p className="px-3 py-2 text-xs text-muted-foreground">No files match “{filter.trim()}”.</p>
          )}
          {renderNodes(tree, 0)}
        </div>
        {source && (
          <p className="border-t border-border px-3 py-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            {source === 'workspace' ? 'Live workspace' : 'Committed branch'} · {files.length} files
          </p>
        )}
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {selected && (
          <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-1.5">
            <span className="truncate font-mono text-xs text-foreground">{selected}</span>
            {file && !file.binary && (
              <span className="shrink-0 text-[10px] text-muted-foreground">{formatBytes(file.size)}</span>
            )}
          </div>
        )}
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {!selected && <p className="p-4 text-xs text-muted-foreground">Select a file to view its contents.</p>}
          {selected && loadingFile && <p className="p-4 text-xs text-muted-foreground">Loading file…</p>}
          {selected && fileError && <p className="p-4 text-xs text-destructive">{fileError}</p>}
          {file && file.binary && (
            <p className="p-4 text-xs text-muted-foreground">Binary file ({formatBytes(file.size)}). Its contents are not shown.</p>
          )}
          {file && !file.binary && (
            <>
              {file.truncated && (
                <p className="border-b border-border px-3 py-1.5 text-[11px] text-muted-foreground">
                  Showing the first part of this file ({formatBytes(file.size)} in total).
                </p>
              )}
              <div className="relative min-h-[300px] flex-1" data-testid="file-explorer-content">
                <div className="absolute inset-0">
                  <CodeEditor value={file.content} language={languageFromPath(file.path)} />
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};
