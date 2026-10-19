import { useState } from 'react';
import { taskApi } from '../api/taskApi';

const MESSAGE_MS = 4000;

// The session-level commands (compact the context, interrupt the turn) and the outcome message they report.
export function useSessionActions(taskId: string, refresh: () => Promise<unknown>) {
  const [isCompacting, setIsCompacting] = useState(false);
  const [isInterrupting, setIsInterrupting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const run = async (
    start: (busy: boolean) => void,
    call: () => Promise<unknown>,
    success: string,
    failure: string,
  ) => {
    start(true);
    setMessage(null);
    try {
      await call();
      setMessage(success);
      await refresh();
      setTimeout(() => setMessage(null), MESSAGE_MS);
    } catch (err: unknown) {
      setMessage(`${failure}: ${(err as { message?: string })?.message || 'Unknown error'}`);
    } finally {
      start(false);
    }
  };

  return {
    isCompacting,
    isInterrupting,
    message,
    interrupt: () =>
      run(setIsInterrupting, () => taskApi.interruptTaskSession(taskId), 'Interrupt sent. The current turn is stopping.', 'Interrupt failed'),
    compact: () =>
      run(setIsCompacting, () => taskApi.compactTaskSession(taskId), 'Session context successfully compacted.', 'Compaction failed'),
  };
}
