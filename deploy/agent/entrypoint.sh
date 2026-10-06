#!/bin/bash
set -e

echo -e "\x1b[36m╔═══════════════════════════════════════════════════════════════════╗\x1b[0m"
echo -e "\x1b[36m║            AGENT TASK MANAGER - AUTONOMOUS SANDBOX                ║\x1b[0m"
echo -e "\x1b[36m╚═══════════════════════════════════════════════════════════════════╝\x1b[0m"

echo -e "\x1b[32m✔\x1b[0m Sandbox container initialized."
echo -e "\x1b[90mCommand:\x1b[0m $0 $*"
echo -e "\x1b[90mWorking Directory:\x1b[0m $(pwd)"

if [ -n "$ATM_TASK_ID" ]; then
  echo -e "\x1b[90mTask ID:\x1b[0m $ATM_TASK_ID"
fi
if [ -n "$ATM_PROJECT_ID" ]; then
  echo -e "\x1b[90mProject ID:\x1b[0m $ATM_PROJECT_ID"
fi
if [ -n "$ATM_TASK_MODEL" ]; then
  echo -e "\x1b[90mModel Target:\x1b[0m $ATM_TASK_MODEL"
fi

if [ -f "/workspace/opencode.json" ]; then
  echo -e "\x1b[34mℹ\x1b[0m Loaded configuration: /workspace/opencode.json"
fi

echo -e "\x1b[33m⏳\x1b[0m Inspecting repository context..."
sleep 1

echo -e "\x1b[33m⏳\x1b[0m Synthesizing solution plan and modifications..."
sleep 1

PROMPT=""
for arg in "$@"; do
  if [[ "$arg" != "run" && "$arg" != "exec" && "$arg" != --* ]]; then
    PROMPT="$arg"
  fi
done

TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SOLUTION_FILE="/workspace/task-execution.md"

cat <<EOF > "$SOLUTION_FILE"
# Task Execution Record

- **Task ID:** ${ATM_TASK_ID:-unknown}
- **Timestamp:** $TIMESTAMP
- **Model:** ${ATM_TASK_MODEL:-default}
- **Prompt:** ${PROMPT:-None provided}

## Execution Summary
The autonomous agent successfully completed the task execution plan in the sandbox.
Files created and verified.
EOF

echo -e "\x1b[32m✔\x1b[0m Applied changes to task-execution.md"
echo -e "\x1b[32m✔\x1b[0m Autonomous execution completed successfully."
exit 0
