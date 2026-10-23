import React, { useEffect, useRef, useState, useMemo } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { useTheme, type TerminalTheme } from '@/components/theme/ThemeProvider';
import { useTaskStore } from '../store/useTaskStore';
import { parseWsFrames } from '../utils/terminalParser';
import type { TaskStatus } from '../schemas/taskSchemas';
import { Trash2, Copy, Check, Terminal as TerminalIcon, Palette } from 'lucide-react';

export interface TerminalViewProps {
  taskId: string;
}

const DARK_TERMINAL_THEME = {
  background: '#090d16',
  foreground: '#e6edf3',
  cursor: '#58a6ff',
  cursorAccent: '#090d16',
  selectionBackground: '#264f78',
  black: '#0d1117',
  red: '#fa7970',
  green: '#7ce38b',
  yellow: '#faa356',
  blue: '#77bdfb',
  magenta: '#cea5fb',
  cyan: '#a2d2fb',
  white: '#b1bac4',
  brightBlack: '#6e7681',
  brightRed: '#ffa198',
  brightGreen: '#56d364',
  brightYellow: '#e3b341',
  brightBlue: '#79c0ff',
  brightMagenta: '#d2a8ff',
  brightCyan: '#56d4dd',
  brightWhite: '#f0f6fc',
};

const LIGHT_TERMINAL_THEME = {
  background: '#ffffff',
  foreground: '#0f172a',
  cursor: '#2563eb',
  cursorAccent: '#ffffff',
  selectionBackground: '#bfdbfe',
  black: '#0f172a',
  red: '#dc2626',
  green: '#16a34a',
  yellow: '#ca8a04',
  blue: '#2563eb',
  magenta: '#9333ea',
  cyan: '#0891b2',
  white: '#f1f5f9',
  brightBlack: '#64748b',
  brightRed: '#ef4444',
  brightGreen: '#22c55e',
  brightYellow: '#eab308',
  brightBlue: '#3b82f6',
  brightMagenta: '#a855f7',
  brightCyan: '#06b6d4',
  brightWhite: '#020617',
};

const MATRIX_TERMINAL_THEME = {
  background: '#050a05',
  foreground: '#00ff66',
  cursor: '#00ff66',
  cursorAccent: '#050a05',
  selectionBackground: '#003b12',
  black: '#001a00',
  red: '#ff3333',
  green: '#00ff66',
  yellow: '#ffcc00',
  blue: '#3399ff',
  magenta: '#cc33ff',
  cyan: '#00ffff',
  white: '#ccffcc',
  brightBlack: '#005500',
  brightRed: '#ff6666',
  brightGreen: '#66ff99',
  brightYellow: '#ffff66',
  brightBlue: '#66b3ff',
  brightMagenta: '#e066ff',
  brightCyan: '#66ffff',
  brightWhite: '#ffffff',
};

export const TerminalView: React.FC<TerminalViewProps> = ({ taskId }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  const { resolvedTheme, terminalTheme, setTerminalTheme } = useTheme();
  const [isConnected, setIsConnected] = useState(false);
  const [copied, setCopied] = useState(false);

  // Compute active terminal theme palette
  const activePalette = useMemo(() => {
    if (terminalTheme === 'matrix') return MATRIX_TERMINAL_THEME;
    if (terminalTheme === 'dark') return DARK_TERMINAL_THEME;
    if (terminalTheme === 'light') return LIGHT_TERMINAL_THEME;
    return resolvedTheme === 'dark' ? DARK_TERMINAL_THEME : LIGHT_TERMINAL_THEME;
  }, [terminalTheme, resolvedTheme]);

  // Dynamically update xterm options when theme changes
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.options.theme = activePalette;
    }
  }, [activePalette]);

  useEffect(() => {
    if (!containerRef.current) return;

    let term: Terminal | null = null;
    let fitAddon: FitAddon | null = null;

    try {
      term = new Terminal({
        theme: activePalette,
        fontFamily: 'ui-monospace, SFMono-Regular, "Geist Mono", Menlo, Monaco, Consolas, "Liberation Mono", monospace',
        fontSize: 12.5,
        lineHeight: 1.35,
        cursorBlink: true,
        convertEol: true,
        scrollback: 10000,
        allowTransparency: false,
      });

      fitAddon = new FitAddon();
      term.loadAddon(fitAddon);

      term.open(containerRef.current);

      // Fit after DOM render
      requestAnimationFrame(() => {
        try {
          fitAddon?.fit();
        } catch {
          // ignore
        }
      });

      terminalRef.current = term;
      fitAddonRef.current = fitAddon;

      term.writeln(`\x1b[90mConnecting to execution stream for task ${taskId}...\x1b[0m`);
    } catch (e) {
      console.warn('Terminal initialization failed or running in non-canvas environment:', e);
    }

    // Connect WebSocket for this task's terminal session
    try {
      const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
      const wsProtocol = isHttps ? 'wss:' : 'ws:';
      const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
      const wsUrl = `${wsProtocol}//${host}/api/v1/ws/terminal/${encodeURIComponent(taskId)}`;

      const socket = new WebSocket(wsUrl);
      wsRef.current = socket;

      socket.onopen = () => {
        setIsConnected(true);
        if (term) {
          term.writeln(`\x1b[32m✔ Connected to agent stream.\x1b[0m\r\n`);
        }
      };

      socket.onmessage = (event) => {
        if (!term) return;

        const rawData = typeof event.data === 'string' ? event.data : '';
        const frames = parseWsFrames(rawData);

        if (frames.length > 0) {
          for (const frame of frames) {
            if (frame.type === 'log' && typeof frame.payload === 'string') {
              term.write(frame.payload);
            } else if (frame.type === 'status' && typeof frame.payload === 'string') {
              const newStatus = frame.payload as TaskStatus;
              useTaskStore.getState().updateTaskStatus(taskId, newStatus);
              term.writeln(`\r\n\x1b[36m➜ Task status updated to:\x1b[0m \x1b[1;33m${newStatus}\x1b[0m\r\n`);
            } else if (frame.type === 'diff' && typeof frame.payload === 'string') {
              // Task diff updated
            }
          }
        } else {
          // Fallback if raw text or direct ANSI chunk
          term.write(rawData);
        }
        term.scrollToBottom();
      };

      socket.onerror = () => {
        setIsConnected(false);
        if (term) {
          term.writeln(`\x1b[33m⚠ Realtime stream unavailable. Fallback to mock/historical view.\x1b[0m`);
        }
      };

      socket.onclose = () => {
        setIsConnected(false);
        if (term) {
          term.writeln(`\r\n\x1b[90mExecution stream disconnected.\x1b[0m`);
        }
      };
    } catch (e) {
      console.warn('WebSocket connection failed:', e);
      setIsConnected(false);
    }

    // Handle window resize
    const handleResize = () => {
      try {
        fitAddonRef.current?.fit();
      } catch {
        // ignore in mock environments
      }
    };
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);

      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }

      if (terminalRef.current) {
        try {
          terminalRef.current.dispose();
        } catch {
          // ignore
        }
        terminalRef.current = null;
      }
    };
  }, [taskId]);

  const handleClear = () => {
    if (terminalRef.current) {
      terminalRef.current.clear();
    }
  };

  const handleCopy = async () => {
    if (terminalRef.current) {
      terminalRef.current.selectAll();
      const selection = terminalRef.current.getSelection();
      terminalRef.current.clearSelection();

      if (selection && navigator.clipboard) {
        await navigator.clipboard.writeText(selection);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }
    }
  };

  const cycleTerminalTheme = () => {
    const themes: TerminalTheme[] = ['auto', 'dark', 'light', 'matrix'];
    const currentIndex = themes.indexOf(terminalTheme);
    const nextTheme = themes[(currentIndex + 1) % themes.length];
    setTerminalTheme(nextTheme);
  };

  return (
    <div className="flex flex-col h-full w-full rounded-lg border border-border bg-card overflow-hidden text-sm shadow-xs transition-colors">
      {/* Terminal Toolbar */}
      <div className="flex items-center justify-between px-3 py-2 bg-muted/50 border-b border-border select-none">
        <div className="flex items-center gap-2">
          <TerminalIcon className="size-4 text-muted-foreground" />
          <span className="font-mono text-xs font-medium text-foreground">terminal_session</span>
          <Badge
            variant={isConnected ? 'default' : 'outline'}
            className={
              isConnected
                ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-[10px] py-0'
                : 'text-[10px] text-muted-foreground py-0'
            }
          >
            <span
              className={`size-1.5 rounded-full mr-1 ${
                isConnected ? 'bg-emerald-500 animate-pulse' : 'bg-muted-foreground'
              }`}
            />
            {isConnected ? 'Connected' : 'Disconnected'}
          </Badge>
        </div>

        <div className="flex items-center gap-1.5">
          {/* Quick Terminal Theme Cycle Button */}
          <Button
            size="xs"
            variant="ghost"
            onClick={cycleTerminalTheme}
            className="h-6 text-xs text-muted-foreground hover:text-foreground gap-1 px-2"
            title={`Current terminal theme: ${terminalTheme} (Click to switch)`}
          >
            <Palette className="size-3 text-muted-foreground" />
            <span className="text-[11px] capitalize">{terminalTheme}</span>
          </Button>

          <Button
            size="xs"
            variant="ghost"
            onClick={handleCopy}
            className="h-6 text-xs text-muted-foreground hover:text-foreground"
          >
            {copied ? <Check className="size-3 text-emerald-500 mr-1" /> : <Copy className="size-3 mr-1" />}
            {copied ? 'Copied' : 'Copy'}
          </Button>
          <Button
            size="xs"
            variant="ghost"
            onClick={handleClear}
            className="h-6 text-xs text-muted-foreground hover:text-foreground"
          >
            <Trash2 className="size-3 mr-1" />
            Clear
          </Button>
        </div>
      </div>

      {/* Terminal Canvas Container */}
      <div
        ref={containerRef}
        data-testid="terminal-container"
        style={{ backgroundColor: activePalette.background }}
        className="flex-1 w-full min-h-[300px] p-2 overflow-hidden transition-colors"
      />
    </div>
  );
};
