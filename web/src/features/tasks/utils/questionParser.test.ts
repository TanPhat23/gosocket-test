import { describe, it, expect } from 'vitest';
import { extractQuestionsFromText } from './questionParser';

describe('extractQuestionsFromText', () => {
  it('extracts multi-question questionnaire from Image 2 agent text', () => {
    const agentMarkdown = `
### Classification: Bounded Task

The project is a standard Next.js (App Router, Tailwind CSS v4, React 19) template application. We want to add a hero banner section to the UI.

Before proceeding, let's clarify your preferences:

1. **Placement & Scope:**
- **Option A (Recommended):** Create a dedicated, clean hero banner section on \`src/app/page.tsx\` (headline, subheading/tagline, primary and secondary call-to-action buttons, badge/pill), replacing or integrating cleanly with the default Next.js starter layout.
- **Option B:** Add a reusable \`src/components/HeroBanner.tsx\` component and mount it at the top of \`src/app/page.tsx\`.

2. **Copy & Purpose:**
- What is the app's topic or brand name, or would you like a modern tech/product placeholder (e.g. "Build faster with Agent Task Manager")?

Which direction do you prefer?
`;

    const questions = extractQuestionsFromText(agentMarkdown);

    expect(questions).toHaveLength(2);

    // Question 1
    expect(questions[0].header).toBe('1. Placement & Scope');
    expect(questions[0].question).toBe('Placement & Scope');
    expect(questions[0].options).toHaveLength(2);
    expect(questions[0].options?.[0].label).toBe('Option A (Recommended)');
    expect(questions[0].options?.[0].description).toContain('Create a dedicated, clean hero banner section');
    expect(questions[0].options?.[1].label).toBe('Option B');
    expect(questions[0].options?.[1].description).toContain('Add a reusable');

    // Question 2
    expect(questions[1].header).toBe('2. Copy & Purpose');
    expect(questions[1].question).toContain("What is the app's topic or brand name");
    expect(questions[1].options).toHaveLength(2);
    expect(questions[1].options?.[0].label).toBe('Build faster with Agent Task Manager (Recommended)');
    expect(questions[1].options?.[1].label).toBe('Custom / Other');
  });

  it('extracts single question with options', () => {
    const text = `
Before proceeding, let's clarify your preferences:
- **Option 1 (Recommended):** Modern Tailwind Dark Theme
- **Option 2:** Minimal Light Theme
`;

    const questions = extractQuestionsFromText(text);
    expect(questions).toHaveLength(1);
    expect(questions[0].options).toHaveLength(2);
    expect(questions[0].options?.[0].label).toBe('Option 1 (Recommended)');
    expect(questions[0].options?.[1].label).toBe('Option 2');
  });

  it('extracts lettered options: A) and B)', () => {
    const text = `
1. **Routing Strategy**
- A) File-based routing with TanStack Router
- B) Custom path matching
`;

    const questions = extractQuestionsFromText(text);
    expect(questions).toHaveLength(1);
    expect(questions[0].options).toHaveLength(2);
    expect(questions[0].options?.[0].label).toBe('A)');
    expect(questions[0].options?.[1].label).toBe('B)');
  });

  it('returns empty array for plain text or standard step lists without options', () => {
    const stepsText = `
Here is our execution plan:
1. Run database migrations
2. Build frontend assets
3. Deploy to production
`;

    const questions = extractQuestionsFromText(stepsText);
    expect(questions).toHaveLength(0);
  });

  it('turns a plain numbered list under a question into a single-choice question', () => {
    const text = `Implementation complete. What would you like to do?

1. Merge back to main locally
2. Push and create a Pull Request
3. Keep the branch as-is (I'll handle it later)

Which option?`;

    const questions = extractQuestionsFromText(text);
    expect(questions).toHaveLength(1);
    expect(questions[0].question).toBe('Implementation complete. What would you like to do?');
    expect(questions[0].multiple).toBe(false);
    expect(questions[0].options?.map((o) => o.label)).toEqual([
      'Merge back to main locally',
      'Push and create a Pull Request',
      "Keep the branch as-is (I'll handle it later)",
    ]);
    expect(questions[0].options?.[1].value).toBe('Push and create a Pull Request');
  });

  it('accepts a numbered list whose only question cue comes after it', () => {
    const questions = extractQuestionsFromText(`Here are the options:
1. Squash and merge
2. Rebase and merge

Which one should I use?`);
    expect(questions).toHaveLength(1);
    expect(questions[0].question).toBe('Which one should I use?');
    expect(questions[0].options).toHaveLength(2);
  });

  it('does not treat a lone numbered item or a question-less list as a choice', () => {
    expect(extractQuestionsFromText('Should I continue?\n1. Yes')).toHaveLength(0);
    expect(extractQuestionsFromText('What happened:\n1. We built it\n2. We shipped it')).toHaveLength(0);
  });

  it('only uses the last numbered list when several appear', () => {
    const questions = extractQuestionsFromText(`Steps done:
1. Built
2. Tested

What next?
1. Merge
2. Open a PR`);
    expect(questions).toHaveLength(1);
    expect(questions[0].options?.map((o) => o.label)).toEqual(['Merge', 'Open a PR']);
  });

  it('handles null, undefined or empty strings safely', () => {
    expect(extractQuestionsFromText('')).toEqual([]);
    expect(extractQuestionsFromText(null as unknown as string)).toEqual([]);
    expect(extractQuestionsFromText(undefined as unknown as string)).toEqual([]);
  });
});
