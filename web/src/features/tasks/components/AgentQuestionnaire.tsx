import * as React from 'react';
import {
  Questionnaire,
  QuestionnaireActions,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireDescription,
  QuestionnaireError,
  QuestionnaireInput,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from '@/components/ui/questionnaire';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { HelpCircle, CheckCircle2, Loader2, Send } from 'lucide-react';

export interface QuestionOption {
  label: string;
  description?: string;
  value?: string;
}

export interface QuestionItemData {
  header?: string;
  question: string;
  options?: QuestionOption[];
  choices?: QuestionOption[];
  multiple?: boolean;
}

export interface AgentQuestionnaireProps {
  questions: QuestionItemData[];
  status?: string;
  output?: string | Record<string, unknown>;
  onSubmitAnswer?: (formattedAnswer: string) => Promise<void> | void;
  isSubmitting?: boolean;
}

export function normalizeQuestionItems(raw: unknown): QuestionItemData[] {
  if (!raw) return [];

  if (Array.isArray(raw)) {
    return raw
      .map((item) => normalizeSingleQuestion(item))
      .filter((q): q is QuestionItemData => Boolean(q && q.question));
  }

  if (typeof raw === 'object' && raw !== null) {
    const obj = raw as Record<string, unknown>;
    if (Array.isArray(obj.questions)) {
      return obj.questions
        .map((item) => normalizeSingleQuestion(item))
        .filter((q): q is QuestionItemData => Boolean(q && q.question));
    }
    const single = normalizeSingleQuestion(raw);
    if (single && single.question) {
      return [single];
    }
  }

  return [];
}

function normalizeSingleQuestion(raw: unknown): QuestionItemData | null {
  if (typeof raw !== 'object' || raw === null) return null;
  const obj = raw as Record<string, unknown>;

  const question = typeof obj.question === 'string' ? obj.question : '';
  const header = typeof obj.header === 'string' ? obj.header : undefined;
  const multiple = Boolean(obj.multiple);

  const rawOptions = Array.isArray(obj.options)
    ? obj.options
    : Array.isArray(obj.choices)
    ? obj.choices
    : [];

  const options: QuestionOption[] = rawOptions.map((opt, idx) => {
    if (typeof opt === 'string') {
      return { label: opt, value: opt };
    }
    if (typeof opt === 'object' && opt !== null) {
      const o = opt as Record<string, unknown>;
      const label = String(o.label || o.value || `Option ${idx + 1}`);
      const value = String(o.value || label);
      const description = typeof o.description === 'string' ? o.description : undefined;
      return { label, value, description };
    }
    return { label: `Option ${idx + 1}`, value: String(idx) };
  });

  return {
    question,
    header,
    multiple,
    options,
  };
}

export function AgentQuestionnaire({
  questions: rawQuestions,
  status,
  output,
  onSubmitAnswer,
  isSubmitting = false,
}: AgentQuestionnaireProps) {
  const [localSubmittedAnswer, setLocalSubmittedAnswer] = React.useState<string | null>(null);
  const [submitError, setSubmitError] = React.useState<string | null>(null);

  const items = React.useMemo(() => {
    return normalizeQuestionItems(rawQuestions);
  }, [rawQuestions]);

  const isCompleted =
    Boolean(localSubmittedAnswer) ||
    status === 'completed' ||
    (Boolean(output) && typeof output === 'string' && output.trim().length > 0);

  if (items.length === 0) {
    return null;
  }

  const questionnaireItems = items.map((q, idx) => ({
    name: `question_${idx}`,
    required: false,
    disabled: isCompleted || isSubmitting,
    choices: (q.options || []).map((opt) => ({
      value: opt.value || opt.label,
      disabled: isCompleted || isSubmitting,
    })),
  }));

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (isCompleted || isSubmitting) return;

    setSubmitError(null);
    const formData = new FormData(e.currentTarget);
    const answerParts: string[] = [];

    items.forEach((q, idx) => {
      const name = `question_${idx}`;
      const selected = formData.getAll(name).map(String).filter(Boolean);
      const customVal = formData.get(`${name}_custom`)?.toString().trim();
      if (customVal) {
        selected.push(customVal);
      }

      const resolvedAnswers = selected.map((val) => {
        const found = q.options?.find((opt) => (opt.value || opt.label) === val);
        return found ? found.label : val;
      });

      if (resolvedAnswers.length > 0) {
        if (items.length > 1) {
          answerParts.push(`Answer for "${q.question}": ${resolvedAnswers.join(', ')}`);
        } else {
          answerParts.push(resolvedAnswers.join(', '));
        }
      }
    });

    if (answerParts.length === 0) {
      setSubmitError('Please select an option or enter an answer.');
      return;
    }

    const fullAnswerText = answerParts.join('\n');
    setLocalSubmittedAnswer(fullAnswerText);

    if (onSubmitAnswer) {
      try {
        await onSubmitAnswer(fullAnswerText);
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : 'Failed to submit response';
        setSubmitError(message);
        setLocalSubmittedAnswer(null);
      }
    }
  };

  const headerTitle = items[0]?.header || 'Agent Clarification Needed';

  return (
    <Card
      data-slot="agent-questionnaire"
      className="border-primary/30 bg-card/95 shadow-sm overflow-hidden"
      data-testid="agent-questionnaire-card"
    >
      <CardHeader className="pb-3 border-b border-border/60 bg-muted/20">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <div className="p-1 rounded-md bg-primary/10 text-primary">
              <HelpCircle className="size-4" />
            </div>
            <div>
              <CardTitle className="text-sm font-semibold tracking-tight">
                {headerTitle}
              </CardTitle>
              <CardDescription className="text-xs">
                {isCompleted
                  ? 'Your response was submitted to the agent.'
                  : 'Please answer the questionnaire to help the agent proceed.'}
              </CardDescription>
            </div>
          </div>
          <div>
            {isCompleted ? (
              <Badge
                variant="outline"
                className="bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 font-mono text-[10px] flex items-center gap-1"
                data-testid="questionnaire-completed-badge"
              >
                <CheckCircle2 className="size-3" />
                Submitted
              </Badge>
            ) : (
              <Badge
                variant="outline"
                className="bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30 font-mono text-[10px] flex items-center gap-1"
                data-testid="questionnaire-pending-badge"
              >
                <span className="size-1.5 rounded-full bg-amber-500 animate-pulse" />
                Input Needed
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>

      <CardContent className="pt-4">
        {isCompleted && (localSubmittedAnswer || typeof output === 'string') && (
          <div
            className="mb-4 p-3 rounded-lg bg-emerald-500/5 border border-emerald-500/20 text-xs font-mono text-foreground space-y-1"
            data-testid="submitted-answer-preview"
          >
            <div className="text-[10px] uppercase font-semibold text-emerald-600 dark:text-emerald-400 tracking-wider">
              Submitted Response
            </div>
            <p className="whitespace-pre-wrap">{localSubmittedAnswer || String(output)}</p>
          </div>
        )}

        <Questionnaire
          items={questionnaireItems}
          onSubmit={handleSubmit}
          className="space-y-4"
        >
          {items.length > 1 && <QuestionnaireProgress className="text-xs font-mono" />}

          {items.map((q, idx) => {
            const name = `question_${idx}`;
            const options = q.options || [];

            return (
              <QuestionnaireItem
                key={name}
                name={name}
                multiple={q.multiple}
                required={false}
                disabled={isCompleted || isSubmitting}
                className="gap-3"
                data-testid={`question-item-${idx}`}
              >
                <div>
                  <QuestionnaireTitle className="text-sm font-medium text-foreground">
                    {q.question}
                  </QuestionnaireTitle>
                  {q.header && (
                    <QuestionnaireDescription className="text-xs text-muted-foreground mt-0.5">
                      {q.header}
                    </QuestionnaireDescription>
                  )}
                </div>

                <QuestionnaireChoices className="gap-2">
                  {options.map((opt, optIdx) => {
                    const value = opt.value || opt.label;
                    const isRecommended =
                      opt.label.includes('(Recommended)') ||
                      (optIdx === 0 && !options.some((o) => o.label.includes('(Recommended)')));

                    return (
                      <QuestionnaireChoice
                        key={value}
                        value={value}
                        disabled={isCompleted || isSubmitting}
                        className="p-3 rounded-lg border-border/80 hover:bg-muted/30 transition-colors"
                        data-testid={`choice-${idx}-${optIdx}`}
                      >
                        <div className="flex items-center justify-between gap-2 w-full">
                          <span className="font-medium text-xs text-foreground">
                            {opt.label.replace(' (Recommended)', '')}
                          </span>
                          {isRecommended && (
                            <Badge
                              variant="secondary"
                              className="text-[9px] px-1.5 py-0 font-normal bg-primary/10 text-primary border-primary/20 shrink-0"
                            >
                              Recommended
                            </Badge>
                          )}
                        </div>
                        {opt.description && (
                          <QuestionnaireChoiceDescription className="text-[11px] text-muted-foreground mt-0.5">
                            {opt.description}
                          </QuestionnaireChoiceDescription>
                        )}
                      </QuestionnaireChoice>
                    );
                  })}

                  {!isCompleted && (
                    <div className="pt-1">
                      <QuestionnaireInput
                        placeholder="Or type a custom answer..."
                        disabled={isCompleted || isSubmitting}
                        aria-label={`Custom answer for ${q.question}`}
                        data-testid={`custom-input-${idx}`}
                      />
                    </div>
                  )}
                </QuestionnaireChoices>

                <QuestionnaireError />
              </QuestionnaireItem>
            );
          })}

          {submitError && (
            <p className="text-xs text-destructive font-mono" data-testid="questionnaire-error">
              {submitError}
            </p>
          )}

          {!isCompleted && (
            <QuestionnaireActions className="pt-2">
              {items.length > 1 && (
                <>
                  <QuestionnairePrevious size="sm" variant="outline">
                    Previous
                  </QuestionnairePrevious>
                  <QuestionnaireNext size="sm" variant="outline">
                    Next
                  </QuestionnaireNext>
                </>
              )}
              <QuestionnaireSubmit
                size="sm"
                variant="default"
                disabled={isSubmitting}
                className="gap-1.5 font-medium cursor-pointer"
                data-testid="questionnaire-submit-btn"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="size-3.5 animate-spin" />
                    Submitting...
                  </>
                ) : (
                  <>
                    <Send className="size-3.5" />
                    Submit Answer & Continue
                  </>
                )}
              </QuestionnaireSubmit>
            </QuestionnaireActions>
          )}
        </Questionnaire>
      </CardContent>
    </Card>
  );
}
