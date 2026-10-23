import { CheckCircle2, Clock, Sparkles, XCircle } from 'lucide-react';
import { Badge } from '@/components/ui/badge';

export function SessionStatusBadge({ status }: { status?: string }) {
  const s = (status || 'running').toLowerCase();
  if (s === 'succeeded' || s === 'completed' || s === 'done') {
    return (
      <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-[10px] font-mono">
        <CheckCircle2 className="size-3 mr-1 inline" />
        {s}
      </Badge>
    );
  }
  if (s === 'failed' || s === 'error') {
    return (
      <Badge variant="destructive" className="text-[10px] font-mono">
        <XCircle className="size-3 mr-1 inline" />
        {s}
      </Badge>
    );
  }
  if (s === 'idle') {
    return (
      <Badge variant="outline" className="text-muted-foreground text-[10px] font-mono">
        <Clock className="size-3 mr-1 inline" />
        idle
      </Badge>
    );
  }
  return (
    <Badge className="bg-blue-500/15 text-blue-600 dark:text-blue-400 border-blue-500/30 text-[10px] font-mono animate-pulse">
      <Sparkles className="size-3 mr-1 inline" />
      {s}
    </Badge>
  );
}
