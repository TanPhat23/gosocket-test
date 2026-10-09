import type { QuestionItemData, QuestionOption } from '../components/AgentQuestionnaire';

/**
 * Extracts structured questionnaire questions from assistant message text when
 * an agent outputs multiple-choice clarification questions directly in markdown.
 */
export function extractQuestionsFromText(text: string): QuestionItemData[] {
  if (!text || typeof text !== 'string') return [];

  const lines = text.split('\n');
  const questions: QuestionItemData[] = [];

  let currentHeader: string | undefined = undefined;
  let currentQuestionText = '';
  let currentOptions: QuestionOption[] = [];

  const flushCurrent = () => {
    if (currentQuestionText.trim() && currentOptions.length > 0) {
      questions.push({
        header: currentHeader,
        question: currentQuestionText.trim(),
        options: currentOptions,
        multiple: false,
      });
    }
    currentHeader = undefined;
    currentQuestionText = '';
    currentOptions = [];
  };

  // Pattern for numbered questions: "1. **Placement & Scope:**" or "1. Placement & Scope"
  const numberedHeadingRegex = /^\s*(?:#{1,4}\s*)?(\d+)[\.\)]\s*(?:\*\*(.*?)\*\*|(.*?))(?::)?\s*$/;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const trimmed = rawLine.trim();
    if (!trimmed) continue;

    // Check if line is a numbered question header
    const numMatch = trimmed.match(numberedHeadingRegex);
    if (numMatch) {
      // Flush previous question if it has options
      flushCurrent();

      const numStr = numMatch[1];
      const title = (numMatch[2] || numMatch[3] || '').trim().replace(/:$/, '');
      currentHeader = `${numStr}. ${title}`;
      currentQuestionText = title;
      continue;
    }

    // Check if line is a bullet: "- ", "* ", "+ "
    const isBullet = trimmed.startsWith('- ') || trimmed.startsWith('* ') || trimmed.startsWith('+ ');
    if (isBullet) {
      const bulletContent = trimmed.slice(2).trim();

      // Check if bullet starts with bold prefix e.g.:
      // "**Option A (Recommended):** description" or "**Option 1:** description"
      const boldMatch = bulletContent.match(/^\*\*(.*?)\*\*[:\s]*(.*)$/);
      if (boldMatch) {
        const rawLabel = boldMatch[1].replace(/[:\s]+$/, '').trim();
        const desc = boldMatch[2].trim();
        const isOption =
          /^option/i.test(rawLabel) ||
          /^choice/i.test(rawLabel) ||
          /^[a-z0-9]\)/i.test(rawLabel) ||
          currentOptions.length > 0;

        if (isOption) {
          currentOptions.push({
            label: rawLabel,
            description: desc || undefined,
            value: desc ? `${rawLabel}: ${desc}` : rawLabel,
          });
          continue;
        }
      }

      // Check for plain option prefix: "Option A:" or "Choice 1:" or "A) "
      const plainPrefixMatch = bulletContent.match(/^(Option\s+[A-Za-z0-9]+(?:\s*\([^)]*\))?|Choice\s+[A-Za-z0-9]+|[A-Za-z0-9]\))\s*[:\-]?\s*(.*)$/i);
      if (plainPrefixMatch) {
        const label = plainPrefixMatch[1].trim();
        const desc = plainPrefixMatch[2].trim();
        currentOptions.push({
          label,
          description: desc || undefined,
          value: desc ? `${label}: ${desc}` : label,
        });
        continue;
      }

      // If bullet does not start with Option, but we are inside a numbered question block:
      if (currentQuestionText && currentOptions.length === 0) {
        // This bullet may be the explanatory question prompt (e.g. Question 2 in Image 2)
        const questionPrompt = bulletContent.trim();
        currentQuestionText = questionPrompt;

        // Check if question prompt mentions a suggested placeholder (e.g. e.g. "Build faster with Agent Task Manager")
        const egMatch = questionPrompt.match(/e\.g\.?\s*["']([^"']+)["']/i);
        if (egMatch) {
          const placeholder = egMatch[1];
          currentOptions.push({
            label: `${placeholder} (Recommended)`,
            description: 'Recommended placeholder value',
            value: placeholder,
          });
          currentOptions.push({
            label: 'Custom / Other',
            description: 'Specify a custom value in the text input below',
            value: 'Custom',
          });
        }
        continue;
      }

      // If there are already options in the current question, treat any additional bullet as an option
      if (currentQuestionText && currentOptions.length > 0) {
        currentOptions.push({
          label: bulletContent,
          value: bulletContent,
        });
        continue;
      }
    }

    // Check for standalone clarification intro without numbered items:
    // e.g. "Before proceeding, let's clarify your preferences:" or "Which option do you prefer?"
    if (
      !currentQuestionText &&
      (trimmed.toLowerCase().includes('clarify your preferences') ||
       trimmed.toLowerCase().includes('which direction do you prefer') ||
       trimmed.toLowerCase().includes('which option do you prefer') ||
       trimmed.toLowerCase().includes('choose an option'))
    ) {
      currentQuestionText = trimmed.replace(/:$/, '');
      currentHeader = 'Clarification';
    }
  }

  // Flush final question
  flushCurrent();

  // If questions were found, return them
  return questions;
}
