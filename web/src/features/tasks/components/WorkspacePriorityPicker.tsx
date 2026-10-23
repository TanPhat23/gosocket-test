import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';

const PRIORITIES = [
  { value: 25, label: 'Low', score: 'P25', desc: 'Background and non-critical work', dot: 'bg-sky-500' },
  { value: 50, label: 'Normal', score: 'P50', desc: 'Standard task execution', dot: 'bg-emerald-500' },
  { value: 80, label: 'High', score: 'P80', desc: 'Urgent fixes and prioritized work', dot: 'bg-amber-500' },
] as const;

// Three priority levels as one segmented choice; the descriptions wrap instead of being cut off.
export function WorkspacePriorityPicker() {
  const defaultPriority = useWorkspaceSettingsStore((s) => s.defaultPriority);
  const setDefaultPriority = useWorkspaceSettingsStore((s) => s.setDefaultPriority);

  return (
    <div role="radiogroup" aria-label="Default Task Priority" className="grid grid-cols-3 gap-2">
      {PRIORITIES.map((p) => {
        const isSelected = defaultPriority === p.value;
        return (
          <button
            key={p.value}
            type="button"
            role="radio"
            aria-checked={isSelected}
            onClick={() => setDefaultPriority(p.value)}
            className={`flex flex-col gap-1 rounded-lg border px-3 py-2 text-left transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40 ${
              isSelected ? 'border-primary bg-primary/10 shadow-xs' : 'border-border/60 bg-background hover:border-border hover:bg-muted/40'
            }`}
          >
            <span className="flex items-center gap-1.5 text-xs font-medium text-foreground">
              <span className={`size-1.5 rounded-full ${p.dot}`} />
              {p.label}
              <span className="font-mono text-[10px] font-normal text-muted-foreground">{p.score}</span>
            </span>
            <span className="text-[10px] leading-snug text-muted-foreground">{p.desc}</span>
          </button>
        );
      })}
    </div>
  );
}
