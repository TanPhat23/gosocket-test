import React, { createContext, useContext, useEffect, useState } from 'react';

export type AppTheme = 'light' | 'dark' | 'system';
export type TerminalTheme = 'auto' | 'dark' | 'light' | 'matrix';

interface ThemeContextType {
  theme: AppTheme;
  setTheme: (theme: AppTheme) => void;
  terminalTheme: TerminalTheme;
  setTerminalTheme: (terminalTheme: TerminalTheme) => void;
  resolvedTheme: 'light' | 'dark';
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

const APP_THEME_STORAGE_KEY = 'atm_theme';
const TERMINAL_THEME_STORAGE_KEY = 'atm_terminal_theme';

export const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [theme, setThemeState] = useState<AppTheme>(() => {
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        const stored = window.localStorage.getItem(APP_THEME_STORAGE_KEY) as AppTheme | null;
        if (stored === 'light' || stored === 'dark' || stored === 'system') return stored;
      }
    } catch {
      // ignore
    }
    return 'dark';
  });

  const [terminalTheme, setTerminalThemeState] = useState<TerminalTheme>(() => {
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        const stored = window.localStorage.getItem(TERMINAL_THEME_STORAGE_KEY) as TerminalTheme | null;
        if (stored === 'auto' || stored === 'dark' || stored === 'light' || stored === 'matrix') return stored;
      }
    } catch {
      // ignore
    }
    return 'auto';
  });

  const [systemIsDark, setSystemIsDark] = useState<boolean>(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true;
    try {
      return window.matchMedia('(prefers-color-scheme: dark)').matches;
    } catch {
      return true;
    }
  });

  // Listen to OS system color-scheme changes
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    try {
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
      const handler = (e: MediaQueryListEvent) => setSystemIsDark(e.matches);

      mediaQuery.addEventListener('change', handler);
      return () => mediaQuery.removeEventListener('change', handler);
    } catch {
      // ignore
    }
  }, []);

  const resolvedTheme: 'light' | 'dark' = theme === 'system' ? (systemIsDark ? 'dark' : 'light') : theme;

  // Apply dark class to document element
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const root = document.documentElement;
    if (resolvedTheme === 'dark') {
      root.classList.add('dark');
      root.style.colorScheme = 'dark';
    } else {
      root.classList.remove('dark');
      root.style.colorScheme = 'light';
    }
  }, [resolvedTheme]);

  const setTheme = (newTheme: AppTheme) => {
    setThemeState(newTheme);
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        window.localStorage.setItem(APP_THEME_STORAGE_KEY, newTheme);
      }
    } catch {
      // ignore
    }
  };

  const setTerminalTheme = (newTerminalTheme: TerminalTheme) => {
    setTerminalThemeState(newTerminalTheme);
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        window.localStorage.setItem(TERMINAL_THEME_STORAGE_KEY, newTerminalTheme);
      }
    } catch {
      // ignore
    }
  };

  return (
    <ThemeContext.Provider
      value={{
        theme,
        setTheme,
        terminalTheme,
        setTerminalTheme,
        resolvedTheme,
      }}
    >
      {children}
    </ThemeContext.Provider>
  );
};

export const useTheme = (): ThemeContextType => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};
