import React from 'react';
import { useTheme, type AppTheme, type TerminalTheme } from './ThemeProvider';
import {
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Badge } from '@/components/ui/badge';
import { Sun, Moon, Laptop, Terminal, Palette, Check } from 'lucide-react';

// Loaded when the theme dialog first opens (see ThemeSettingsDialog).
export const ThemeSettingsDialogContent: React.FC = () => {
  const { theme, setTheme, terminalTheme, setTerminalTheme, resolvedTheme } = useTheme();

  const appThemeOptions: Array<{ id: AppTheme; label: string; desc: string; icon: React.ComponentType<{ className?: string }> }> = [
    { id: 'dark', label: 'Dark Mode', desc: 'Dark backgrounds with high-contrast text', icon: Moon },
    { id: 'light', label: 'Light Mode', desc: 'Crisp light aesthetic for bright environments', icon: Sun },
    { id: 'system', label: 'System Match', desc: 'Automatically matches your OS preference', icon: Laptop },
  ];

  const terminalThemeOptions: Array<{ id: TerminalTheme; label: string; desc: string; previewBg: string; previewFg: string }> = [
    { id: 'auto', label: 'Auto (Match App)', desc: 'Follows current application appearance', previewBg: resolvedTheme === 'dark' ? '#090d16' : '#ffffff', previewFg: resolvedTheme === 'dark' ? '#58a6ff' : '#2563eb' },
    { id: 'dark', label: 'Midnight Dark', desc: 'Deep dark palette with neon accents', previewBg: '#090d16', previewFg: '#7ce38b' },
    { id: 'light', label: 'Paper Light', desc: 'Clean high-contrast terminal for light mode', previewBg: '#f8fafc', previewFg: '#16a34a' },
    { id: 'matrix', label: 'Cyber Matrix', desc: 'Phosphor green terminal on pitch black', previewBg: '#050a05', previewFg: '#00ff66' },
  ];

  return (
    <DialogContent className="sm:max-w-md p-6">
      <DialogHeader className="pb-3 border-b border-border">
        <div className="flex items-center justify-between pr-6">
          <div className="flex items-center gap-2">
            <Palette className="size-4 text-primary" />
            <DialogTitle className="text-base font-semibold">Appearance &amp; Theme Settings</DialogTitle>
          </div>
          <Badge variant="outline" className="font-mono text-[10px] capitalize">
            Active: {resolvedTheme}
          </Badge>
        </div>
      </DialogHeader>

      <div className="space-y-6 pt-2">
        {/* App Appearance Section */}
        <div className="space-y-2.5">
          <div>
            <h4 className="text-xs font-semibold text-foreground tracking-wide uppercase">
              Application Theme
            </h4>
            <p className="text-[11px] text-muted-foreground">
              Select the appearance mode for the dashboard and control plane.
            </p>
          </div>

          <div className="grid grid-cols-3 gap-2">
            {appThemeOptions.map((opt) => {
              const isSelected = theme === opt.id;
              const Icon = opt.icon;
              return (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => setTheme(opt.id)}
                  className={`flex flex-col items-center justify-center p-3 rounded-lg border text-center transition-all cursor-pointer ${
                    isSelected
                      ? 'border-primary bg-primary/10 text-primary shadow-xs'
                      : 'border-border bg-card hover:bg-muted/50 text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <Icon className="size-4 mb-1.5" />
                  <span className="text-xs font-semibold">{opt.label}</span>
                </button>
              );
            })}
          </div>
        </div>

        {/* Terminal Appearance Section */}
        <div className="space-y-2.5">
          <div>
            <h4 className="text-xs font-semibold text-foreground tracking-wide uppercase flex items-center gap-1.5">
              <Terminal className="size-3.5" /> Terminal Logs Theme
            </h4>
            <p className="text-[11px] text-muted-foreground">
              Customize the palette and contrast for the embedded xterm session.
            </p>
          </div>

          <div className="grid grid-cols-2 gap-2">
            {terminalThemeOptions.map((opt) => {
              const isSelected = terminalTheme === opt.id;
              return (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => setTerminalTheme(opt.id)}
                  className={`flex items-center justify-between p-2.5 rounded-lg border text-left transition-all cursor-pointer ${
                    isSelected
                      ? 'border-primary bg-primary/10 text-foreground shadow-xs'
                      : 'border-border bg-card hover:bg-muted/50 text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <div
                      className="size-4 rounded-full border border-border/80 flex items-center justify-center shrink-0"
                      style={{ backgroundColor: opt.previewBg }}
                    >
                      <div
                        className="size-1.5 rounded-full"
                        style={{ backgroundColor: opt.previewFg }}
                      />
                    </div>
                    <div>
                      <div className="text-xs font-medium text-foreground leading-tight">
                        {opt.label}
                      </div>
                      <div className="text-[10px] text-muted-foreground truncate max-w-[120px]">
                        {opt.desc}
                      </div>
                    </div>
                  </div>
                  {isSelected && <Check className="size-3.5 text-primary shrink-0" />}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </DialogContent>
  );
};
