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
import { ChevronsUpDown } from 'lucide-react';
import { cn } from 'cn';

export interface SearchableSelectItem {
  value: string;
  label: string;
}

export interface SearchableSelectProps {
  items: SearchableSelectItem[];
  value: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyText?: string;
  disabled?: boolean;
  icon?: React.ReactNode;
  ariaLabel?: string;
  className?: string;
  testId?: string;
}

// A searchable single-choice picker: a Popover holding a Command list. Search matches the label, not the id.
export const SearchableSelect: React.FC<SearchableSelectProps> = ({
  items,
  value,
  onValueChange,
  placeholder = 'Select...',
  searchPlaceholder = 'Search...',
  emptyText = 'No results found.',
  disabled = false,
  icon,
  ariaLabel,
  className,
  testId,
}) => {
  const [open, setOpen] = React.useState(false);
  const selected = items.find((item) => item.value === value);

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
            aria-label={ariaLabel}
            disabled={disabled}
            className={cn(
              'h-7 w-full justify-between gap-1.5 px-2 text-xs font-normal bg-background/50 cursor-pointer',
              className
            )}
            data-testid={testId}
          >
            <span className="flex min-w-0 items-center gap-1.5">
              {icon}
              <span className="truncate">{selected?.label ?? placeholder}</span>
            </span>
            <ChevronsUpDown className="size-3 shrink-0 opacity-50" />
          </Button>
        }
      />
      <PopoverContent className="w-64 p-0" align="start">
        <Command>
          <CommandInput placeholder={searchPlaceholder} className="text-xs h-8" />
          <CommandList>
            <CommandEmpty>{emptyText}</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <CommandItem
                  key={item.value}
                  value={item.value}
                  keywords={[item.label]}
                  data-checked={item.value === value ? 'true' : undefined}
                  onSelect={() => {
                    onValueChange(item.value);
                    setOpen(false);
                  }}
                  className="text-xs"
                >
                  <span className="truncate">{item.label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
};
