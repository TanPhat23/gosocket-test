import React, { useEffect, useState } from 'react';
import { Editor } from '@monaco-editor/react';
import { configureMonaco } from '@/lib/monaco';
import type { CodeEditorProps } from './CodeEditor';

configureMonaco();

// useDarkMode follows the dark class the theme provider puts on the document root. Reading the class directly keeps
// the editor usable outside the provider and lets it change theme while open.
function useDarkMode(): boolean {
  const read = () => document.documentElement.classList.contains('dark');
  const [dark, setDark] = useState(read);
  useEffect(() => {
    const observer = new MutationObserver(() => setDark(read()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);
  return dark;
}

export const CodeEditorBody: React.FC<CodeEditorProps> = ({ value, language }) => {
  const dark = useDarkMode();
  return (
    <Editor
      height="100%"
      value={value}
      language={language}
      theme={dark ? 'vs-dark' : 'vs'}
      options={{
        readOnly: true,
        domReadOnly: true,
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        wordWrap: 'off',
        fontSize: 12,
        automaticLayout: true,
        renderLineHighlight: 'none',
        glyphMargin: false,
        folding: true,
        scrollbar: { alwaysConsumeMouseWheel: false },
      }}
    />
  );
};
