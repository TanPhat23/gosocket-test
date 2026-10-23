import { useSteer } from './SessionContexts';
import { SteeringIndicator } from './SteeringIndicator';

// The live indicator at the end of the transcript while the agent is working on a steer.
export function SteeringSlot() {
  return useSteer().isSteeringActive ? <SteeringIndicator /> : null;
}
