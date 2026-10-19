import { AgentQuestionnaire } from '../AgentQuestionnaire';
import { useSteer } from './SessionContexts';

type QuestionnaireProps = Omit<React.ComponentProps<typeof AgentQuestionnaire>, 'onSubmitAnswer' | 'isSubmitting'>;

// The agent's questionnaire, answering through the steer prompt.
export function SessionQuestionnaire(props: QuestionnaireProps) {
  const { submitAnswer, isSubmitting } = useSteer();
  return <AgentQuestionnaire {...props} onSubmitAnswer={submitAnswer} isSubmitting={isSubmitting} />;
}
