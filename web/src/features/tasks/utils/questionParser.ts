import type { QuestionItemData, QuestionOption } from '../components/AgentQuestionnaire';

const MAX_QUESTION_LENGTH = 500;
const MAX_LABEL_LENGTH = 200;
const MAX_DESCRIPTION_LENGTH = 2000;
const MAX_HEADER_LENGTH = 150;

// Strip C0 and C1 control characters (except common whitespace \t, \n, \r)
const stripControlChars = (str: string): string => {
  return str.replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F-\u009F]/g, '');
};

const sanitize = (str: string, maxLen: number): string => {
  const cleaned = stripControlChars(str).trim();
  return cleaned.length > maxLen ? cleaned.slice(0, maxLen).trimEnd() : cleaned;
};

const stripMarkdown = (s: string) => s.replace(/\*\*|__/g, '').trim();

/**
 * Extracts structured questionnaire questions from assistant message text when
 * an agent outputs multiple-choice clarification questions directly in markdown.
 */
export function extractQuestionsFromText(text: string): QuestionItemData[] {
  if (!text || typeof text !== 'string') return [];

  const lines = text.split('\n');

  // Try parsing the numbered title with bullet descriptions format (e.g. proposed improvements)
  const numberedSections = extractNumberedTitleSections(lines);
  if (numberedSections.length > 0) {
    return numberedSections;
  }

  const questions: QuestionItemData[] = [];

  let currentHeader: string | undefined = undefined;
  let currentQuestionText = '';
  let currentOptions: QuestionOption[] = [];

  const flushCurrent = () => {
    if (currentQuestionText.trim() && currentOptions.length > 0) {
      questions.push({
        header: currentHeader ? sanitize(currentHeader, MAX_HEADER_LENGTH) : undefined,
        question: sanitize(currentQuestionText, MAX_QUESTION_LENGTH),
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
          const sanitizedLabel = sanitize(rawLabel, MAX_LABEL_LENGTH);
          const sanitizedDesc = desc ? sanitize(desc, MAX_DESCRIPTION_LENGTH) : undefined;
          currentOptions.push({
            label: sanitizedLabel,
            description: sanitizedDesc,
            value: sanitizedDesc ? `${sanitizedLabel}: ${sanitizedDesc}` : sanitizedLabel,
          });
          continue;
        }
      }

      // Check for plain option prefix: "Option A:" or "Choice 1:" or "A) "
      const plainPrefixMatch = bulletContent.match(/^(Option\s+[A-Za-z0-9]+(?:\s*\([^)]*\))?|Choice\s+[A-Za-z0-9]+|[A-Za-z0-9]\))\s*[:\-]?\s*(.*)$/i);
      if (plainPrefixMatch) {
        const label = sanitize(plainPrefixMatch[1].trim(), MAX_LABEL_LENGTH);
        const desc = plainPrefixMatch[2].trim();
        const sanitizedDesc = desc ? sanitize(desc, MAX_DESCRIPTION_LENGTH) : undefined;
        currentOptions.push({
          label,
          description: sanitizedDesc,
          value: sanitizedDesc ? `${label}: ${sanitizedDesc}` : label,
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
        const sanitizedBullet = sanitize(bulletContent, MAX_LABEL_LENGTH);
        currentOptions.push({
          label: sanitizedBullet,
          value: sanitizedBullet,
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
  if (questions.length > 0) return questions;

  return extractNumberedChoice(lines);
}

const NUMBERED_ITEM = /^\s*(\d+)[.)]\s+(\S.*)$/;

/**
 * Handles the numbered-title-with-bullet-descriptions pattern, e.g.:
 * 1.
 * Title / Option A:
 * - bullet 1
 * - bullet 2
 *
 * or:
 * 1. Title / Option A:
 * - bullet 1
 *
 * Followed by an explicit trailing choice/direction question.
 */
function extractNumberedTitleSections(lines: string[]): QuestionItemData[] {
  // Check if any trailing question exists that asks for a choice/direction
  let trailingQuestionIdx = -1;
  let trailingQuestionText = '';

  for (let k = lines.length - 1; k >= 0; k--) {
    const trimmed = lines[k].trim();
    if (!trimmed) continue;
    if (trimmed.endsWith('?')) {
      const lower = trimmed.toLowerCase();
      const isChoiceQuestion =
        lower.includes('which direction') ||
        lower.includes('which option') ||
        lower.includes('which one') ||
        lower.includes('what would you like') ||
        lower.includes('how would you like') ||
        lower.includes('should we proceed') ||
        lower.includes('your preferences') ||
        lower.includes('prefer') ||
        lower.includes('or should');

      if (isChoiceQuestion) {
        trailingQuestionIdx = k;
        trailingQuestionText = stripMarkdown(trimmed);
      }
    }
    // Only check the last non-empty line (or up to 2 trailing lines if there are blank lines)
    break;
  }

  if (trailingQuestionIdx === -1) return [];

  // Sequential scan from line 0 up to trailingQuestionIdx
  interface CandidateSection {
    number: number;
    title: string;
    bullets: string[];
  }

  const sections: CandidateSection[] = [];
  let expectedNumber = 1;
  let currentSec: CandidateSection | null = null;
  let firstSectionStartIdx = -1;

  let i = 0;
  while (i < trailingQuestionIdx) {
    const line = lines[i].trim();
    if (!line) {
      i++;
      continue;
    }

    // Check if line is a numbered item or split numbered item
    // Form 1: "1." or "1)" on its own line
    const splitMatch = line.match(/^(\d+)[\.\)]$/);
    // Form 2: "1. Title..." or "1) Title..." (must not be an option/question heading like "1. **Placement & Scope:**" where bullets have option labels)
    const inlineMatch = line.match(/^(\d+)[\.\)]\s*(.*)$/);

    if (splitMatch) {
      const num = parseInt(splitMatch[1], 10);
      if (num === expectedNumber) {
        // Look ahead for title on next non-empty line
        let titleLine = '';
        let nextIdx = i + 1;
        while (nextIdx < trailingQuestionIdx) {
          const l = lines[nextIdx].trim();
          if (l) {
            // Cannot be a bullet or another number
            if (!l.startsWith('- ') && !l.startsWith('* ') && !l.startsWith('+ ') && !l.match(/^\d+[\.\)]/)) {
              titleLine = l;
            }
            break;
          }
          nextIdx++;
        }

        if (titleLine) {
          if (firstSectionStartIdx === -1) firstSectionStartIdx = i;
          if (currentSec) sections.push(currentSec);
          currentSec = {
            number: num,
            title: stripMarkdown(titleLine).replace(/[:\s]+$/, ''),
            bullets: [],
          };
          expectedNumber++;
          i = nextIdx + 1;
          continue;
        }
      }
    } else if (inlineMatch) {
      const num = parseInt(inlineMatch[1], 10);
      const rest = inlineMatch[2].trim();
      if (num === expectedNumber && rest) {
        if (firstSectionStartIdx === -1) firstSectionStartIdx = i;
        if (currentSec) sections.push(currentSec);
        currentSec = {
          number: num,
          title: stripMarkdown(rest).replace(/[:\s]+$/, ''),
          bullets: [],
        };
        expectedNumber++;
        i++;
        continue;
      }
    }

    // Check for bullets belonging to current section
    const isBullet = line.startsWith('- ') || line.startsWith('* ') || line.startsWith('+ ');
    if (isBullet && currentSec) {
      const rawContent = line.slice(2).trim();
      // If any bullet looks like an option label (e.g. "**Option A:**", "**Option 1:**", "Option A:"),
      // then this is a multi-question clarification like Image 2, NOT a single question with numbered title choices.
      if (
        /^\*\*(?:Option|Choice|[a-z0-9]\))/i.test(rawContent) ||
        /^(?:Option|Choice)\s+[A-Za-z0-9]+/i.test(rawContent)
      ) {
        return [];
      }
      const content = stripMarkdown(rawContent);
      if (content) {
        currentSec.bullets.push(content);
      }
      i++;
      continue;
    }

    // If we encounter a non-bullet line when we have a section:
    // If it's another number or empty, handled above. Otherwise, if currentSec has bullets and it's not a bullet,
    // it could be a section divider or intermediate line.
    i++;
  }

  if (currentSec) {
    sections.push(currentSec);
  }

  // Preconditions:
  // Must have at least 2 consecutive sections starting at 1
  if (sections.length < 2) return [];

  // Each section must have at least one bullet description
  for (const s of sections) {
    if (s.bullets.length === 0) return [];
  }

  // Look for safe header in lines before firstSectionStartIdx
  let safeHeader: string | undefined = undefined;
  for (let h = 0; h < firstSectionStartIdx; h++) {
    const l = lines[h].trim();
    if (!l) continue;
    // Don't take lines with question marks or long intros
    const cleaned = stripMarkdown(l).replace(/[:#]/g, '').trim();
    if (cleaned && !l.endsWith('?') && !l.toLowerCase().includes('preferences') && !l.toLowerCase().includes('priorities')) {
      safeHeader = sanitize(cleaned, MAX_HEADER_LENGTH);
      break;
    }
  }

  const options: QuestionOption[] = sections.map((sec) => {
    const label = sanitize(sec.title, MAX_LABEL_LENGTH);
    const joinedDesc = sec.bullets.join('\n');
    const description = joinedDesc ? sanitize(joinedDesc, MAX_DESCRIPTION_LENGTH) : undefined;
    return {
      label,
      value: label,
      description,
    };
  });

  return [
    {
      header: safeHeader,
      question: sanitize(trailingQuestionText, MAX_QUESTION_LENGTH),
      options,
      multiple: false,
    },
  ];
}

/**
 * Handles the common "question, then a numbered list of choices" shape that
 * the bullet-oriented parsing above does not see. A numbered list only counts
 * as a choice when a question (a line ending in "?") sits right before or
 * right after it, so ordinary step lists stay plain text.
 */
function extractNumberedChoice(lines: string[]): QuestionItemData[] {
  let runStart = -1;
  let runEnd = -1;
  let i = 0;
  while (i < lines.length) {
    const first = lines[i].match(NUMBERED_ITEM);
    if (!first || Number(first[1]) !== 1) {
      i++;
      continue;
    }
    let j = i + 1;
    let expected = 2;
    while (j < lines.length) {
      const m = lines[j].match(NUMBERED_ITEM);
      if (m && Number(m[1]) === expected) {
        expected++;
        j++;
      } else {
        break;
      }
    }
    if (j - i >= 2) {
      runStart = i;
      runEnd = j;
    }
    i = j;
  }
  if (runStart < 0) return [];

  const neighbour = (from: number, step: number) => {
    for (let k = from; k >= 0 && k < lines.length; k += step) {
      if (lines[k].trim()) return stripMarkdown(lines[k]);
    }
    return '';
  };
  const intro = neighbour(runStart - 1, -1);
  const trailing = neighbour(runEnd, 1);
  const introIsQuestion = intro.endsWith('?');
  const trailingIsQuestion = trailing.endsWith('?');
  if (!introIsQuestion && !trailingIsQuestion) return [];

  const options: QuestionOption[] = lines.slice(runStart, runEnd).map((line) => {
    const rawLabel = stripMarkdown(line.match(NUMBERED_ITEM)![2]);
    const label = sanitize(rawLabel, MAX_LABEL_LENGTH);
    return { label, value: label };
  });

  const questionRaw = introIsQuestion ? intro : trailing;
  const question = sanitize(questionRaw, MAX_QUESTION_LENGTH);

  return [
    {
      question,
      options,
      multiple: false,
    },
  ];
}
