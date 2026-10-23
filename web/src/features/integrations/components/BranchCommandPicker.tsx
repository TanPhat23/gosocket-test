import * as React from 'react';
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover';
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from '@/components/ui/command';
import { Button } from '@/components/ui/button';
import { GitBranch, ChevronsUpDown, Check, RefreshCw } from 'lucide-react';
import { cn } from 'cn';

export interface BranchCommandPickerProps {
  value: string;
  branches: string[];
  isLoading?: boolean;
  disabled?: boolean;
  onSelect: (branch: string) => void;
  onRefresh?: () => void;
  placeholder?: string;
  testId?: string;
  triggerClassName?: string;
}

export const BranchCommandPicker: React.FC<BranchCommandPickerProps> = ({
  value,
  branches,
  isLoading = false,
  disabled = false,
  onSelect,
  onRefresh,
  placeholder = 'Select branch...',
  testId,
  triggerClassName,
}) => {
  const [open, setOpen] = React.useState(false);

  // If the active value isn't empty, ensure it's in the list even before GitHub fetch finishes
  const branchList = React.useMemo(() => {
    const list = [...branches];
    if (value && !list.includes(value)) {
      list.unshift(value);
    }
    return list;
  }, [branches, value]);

  const displayBranch = value || placeholder;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            role="combobox"
            aria-expanded={open}
            disabled={disabled}
            className={cn(
              'h-7 justify-between gap-1.5 px-2 text-xs font-mono font-normal cursor-pointer bg-background hover:bg-muted/50 border-input/70',
              triggerClassName
            )}
            data-testid={testId}
          >
            <span className="flex items-center gap-1.5 truncate max-w-[160px]">
              <GitBranch className="size-3 text-primary shrink-0" />
              <span className="truncate">{displayBranch}</span>
            </span>
            <ChevronsUpDown className="size-3 shrink-0 opacity-50" />
          </Button>
        }
      />
      <PopoverContent className="w-64 p-0" align="start">
        <Command>
          <CommandInput placeholder="Search branch..." className="text-xs h-8" />
          <CommandList className="max-h-56">
            {isLoading ? (
              <div className="flex items-center justify-center gap-2 py-4 text-xs text-muted-foreground">
                <RefreshCw className="size-3.5 animate-spin text-primary" />
                <span>Fetching branches...</span>
              </div>
            ) : null}
            {!isLoading && <CommandEmpty>No branch found.</CommandEmpty>}
            {!isLoading && (
              <CommandGroup>
                {branchList.map((branch) => {
                  const isSelected = branch === value;
                  return (
                    <CommandItem
                      key={branch}
                      value={branch}
                      data-checked={isSelected ? 'true' : undefined}
                      onSelect={() => {
                        onSelect(branch);
                        setOpen(false);
                      }}
                      className="text-xs font-mono cursor-pointer flex items-center justify-between"
                      data-testid={testId ? `${testId}-item-${branch}` : undefined}
                    >
                      <span className="flex items-center gap-1.5 truncate">
                        <GitBranch className="size-3 text-muted-foreground shrink-0" />
                        <span className="truncate">{branch}</span>
                      </span>
                      {isSelected ? (
                        <Check className="size-3.5 text-primary shrink-0" />
                      ) : null}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            )}
          </CommandList>
          {onRefresh && (
            <div className="border-t border-border/60 p-1 bg-muted/20">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onRefresh();
                }}
                disabled={isLoading}
                className="w-full flex items-center justify-center gap-1.5 py-1 text-[11px] font-medium text-muted-foreground hover:text-foreground hover:bg-muted rounded transition-colors cursor-pointer disabled:opacity-50"
                data-testid={testId ? `${testId}-refresh-btn` : undefined}
              >
                <RefreshCw className={cn('size-3', isLoading && 'animate-spin')} />
                <span>Refresh from GitHub</span>
              </button>
            </div>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  );
};
