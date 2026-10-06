import React, { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Trash2, Copy, Check, Terminal as TerminalIcon } from 'lucide-react';

export interface TerminalViewProps {
  taskId: string;
}

export const TerminalView: React.FC<TerminalViewProps> = ({ taskId }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  const [isConnected, setIsConnected] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!containerRef.current) return;

    // Gracefully handle environments like jsdom where canvas/DOM might not be fully supported
    let term: Terminal | null = null;
    let fitAddon: FitAddon | null = null;

    try {
      term = new Terminal({
        theme: {
          background: '#090d16',
          foreground: '#e6edf3',
          cursor: '#58a6ff',
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
        },
        fontFamily: 'monospace, "Fira Code", "Courier New"',
        fontSize: 12,
        lineHeight: 1.4,
        cursorBlink: true,
        convertEol: true,
      });

      fitAddon = new FitAddon();
      term.loadAddon(fitAddon);

      term.open(containerRef.current);
      fitAddon.fit();

      terminalRef.current = term;
      fitAddonRef.current = fitAddon;

      term.writeln(`\x1b[90mConnecting to execution stream for task ${taskId}...\x1b[0m`);
    } catch (e) {
      // In tests/jsdom or environments where canvas fails
      console.warn('Terminal initialization failed or running in non-canvas environment:', e);
    }

    // Connect WebSocket
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
        if (term) {
          term.write(event.data);
        }
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

    // Handle resize
    const handleResize = () => {
      try {
        fitAddonRef.current?.fit();
      } catch {
        // ignore in mock environments
      }
    };
    window.addEventListener('resize', handleResize);

    // Cleanup
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

  return (
    <div className="flex flex-col h-full w-full rounded-lg border border-border bg-[#090d16] overflow-hidden text-sm">
      {/* Terminal Toolbar */}
      <div className="flex items-center justify-between px-3 py-2 bg-[#101623] border-b border-border/40 select-none">
        <div className="flex items-center gap-2">
          <TerminalIcon className="size-4 text-muted-foreground" />
          <span className="font-mono text-xs text-muted-foreground">terminal_session</span>
          <Badge
            variant={isConnected ? 'default' : 'outline'}
            className={
              isConnected
                ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30 text-[10px] py-0'
                : 'text-[10px] text-muted-foreground py-0'
            }
          >
            <span
              className={`size-1.5 rounded-full mr-1 ${
                isConnected ? 'bg-emerald-400 animate-pulse' : 'bg-muted-foreground'
              }`}
            />
            {isConnected ? 'Connected' : 'Disconnected'}
          </Badge>
        </div>

        <div className="flex items-center gap-1.5">
          <Button
            size="xs"
            variant="ghost"
            onClick={handleCopy}
            className="h-6 text-xs text-muted-foreground hover:text-foreground"
          >
            {copied ? <Check className="size-3 text-emerald-400 mr-1" /> : <Copy className="size-3 mr-1" />}
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
        className="flex-1 w-full min-h-[300px] p-2 overflow-hidden"
      />
    </div>
  );
};
