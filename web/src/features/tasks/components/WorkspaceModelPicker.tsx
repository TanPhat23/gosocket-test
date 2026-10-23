import { Check } from 'lucide-react';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';

// The model name without the "(Active)" tag some providers add; the dot shows that state instead.
const displayName = (name: string) => name.replace(/\s*\(Active\)$/i, '');

// Every model as a card in a two-column grid, so the choice is visible at once without a scrolling list.
export function WorkspaceModelPicker() {
  const defaultModel = useWorkspaceSettingsStore((s) => s.defaultModel);
  const setDefaultModel = useWorkspaceSettingsStore((s) => s.setDefaultModel);
  const models = useWorkspaceSettingsStore((s) => s.availableModels);

  return (
    <div role="radiogroup" aria-label="Default AI Model" className="grid grid-cols-1 gap-2 sm:grid-cols-2">
      {models.map((m) => {
        const isSelected = defaultModel === m.id;
        return (
          <button
            key={m.id}
            type="button"
            role="radio"
            aria-checked={isSelected}
            onClick={() => setDefaultModel(m.id)}
            className={`flex min-w-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40 ${
              isSelected ? 'border-primary bg-primary/10 shadow-xs' : 'border-border/60 bg-background hover:border-border hover:bg-muted/40'
            }`}
          >
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-1.5 text-xs font-medium text-foreground">
                <span className="truncate">{displayName(m.name)}</span>
                {m.active && <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" title="Active provider" />}
              </span>
              <span className="block truncate font-mono text-[10px] text-muted-foreground">{m.id}</span>
            </span>
            {isSelected && <Check className="size-3.5 shrink-0 text-primary" />}
          </button>
        );
      })}
    </div>
  );
}
