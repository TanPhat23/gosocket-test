import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentQuestionnaire, normalizeQuestionItems } from './AgentQuestionnaire';

describe('normalizeQuestionItems', () => {
  it('normalizes single question format', () => {
    const raw = {
      header: 'Design Aesthetic',
      question: 'Which theme would you prefer?',
      options: [
        { label: 'Blue Theme (Recommended)', description: 'Modern deep slate blue' },
        { label: 'Dark Charcoal', description: 'Monochrome high contrast' },
      ],
      multiple: false,
    };

    const normalized = normalizeQuestionItems(raw);
    expect(normalized).toHaveLength(1);
    expect(normalized[0].question).toBe('Which theme would you prefer?');
    expect(normalized[0].header).toBe('Design Aesthetic');
    expect(normalized[0].options).toHaveLength(2);
    expect(normalized[0].options?.[0].label).toBe('Blue Theme (Recommended)');
  });

  it('normalizes multiple questions array format', () => {
    const raw = {
      questions: [
        {
          question: 'Question 1',
          options: [{ label: 'Opt 1' }, { label: 'Opt 2' }],
        },
        {
          question: 'Question 2',
          options: [{ label: 'Opt A' }, { label: 'Opt B' }],
        },
      ],
    };

    const normalized = normalizeQuestionItems(raw);
    expect(normalized).toHaveLength(2);
    expect(normalized[0].question).toBe('Question 1');
    expect(normalized[1].question).toBe('Question 2');
  });
});

describe('AgentQuestionnaire Component', () => {
  const sampleQuestions = [
    {
      header: 'Hero Design',
      question: 'Which visual style should be applied to the Hero banner?',
      options: [
        { label: 'Deep Blue (Recommended)', description: 'Minimalist blue palette' },
        { label: 'Monochrome Slate', description: 'Clean neutral black and white' },
      ],
      multiple: false,
    },
  ];

  it('renders question, choices, and Recommended badge', () => {
    render(<AgentQuestionnaire questions={sampleQuestions} />);

    expect(screen.getAllByText('Hero Design')[0]).toBeInTheDocument();
    expect(
      screen.getByText('Which visual style should be applied to the Hero banner?')
    ).toBeInTheDocument();
    expect(screen.getByText('Deep Blue')).toBeInTheDocument();
    expect(screen.getByText('Recommended')).toBeInTheDocument();
    expect(screen.getByText('Monochrome Slate')).toBeInTheDocument();
    expect(screen.getByTestId('questionnaire-pending-badge')).toBeInTheDocument();
  });

  it('submits selected answer via onSubmitAnswer', async () => {
    const onSubmitAnswer = vi.fn();

    render(
      <AgentQuestionnaire
        questions={sampleQuestions}
        onSubmitAnswer={onSubmitAnswer}
      />
    );

    const deepBlueChoice = screen.getByTestId('choice-0-0');
    fireEvent.click(deepBlueChoice);

    const submitBtn = screen.getByTestId('questionnaire-submit-btn');
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(onSubmitAnswer).toHaveBeenCalledWith('Deep Blue (Recommended)');
    });
  });

  it('renders completed state when completed', () => {
    render(
      <AgentQuestionnaire
        questions={sampleQuestions}
        status="completed"
        output="Deep Blue (Recommended)"
      />
    );

    expect(screen.getByTestId('questionnaire-completed-badge')).toBeInTheDocument();
    expect(screen.getByTestId('submitted-answer-preview')).toHaveTextContent(
      'Deep Blue (Recommended)'
    );
    expect(screen.queryByTestId('questionnaire-submit-btn')).not.toBeInTheDocument();
  });
});
