import { loader } from '@monaco-editor/react';
import * as monaco from 'monaco-editor';
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker';

let configured = false;

// configureMonaco serves Monaco from the app bundle instead of a CDN, so the editor works offline and under a
// strict content policy. It runs once, the first time an editor is rendered.
export function configureMonaco(): void {
  if (configured) return;
  configured = true;
  self.MonacoEnvironment = {
    getWorker: () => new EditorWorker(),
  };
  loader.config({ monaco });
}
