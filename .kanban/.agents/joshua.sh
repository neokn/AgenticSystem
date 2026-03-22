#!/usr/bin/env bash
PIPE=".kanban/.pipes/joshua"
echo "[Joshua] Ready — waiting for tasks"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)

  if [[ "$CARD_PATH" == "TRIAGE" ]]; then
    echo "[Joshua] Running board triage"
    claude "Joshua, the J-Team just started. Survey the board at .kanban/ — check backlog/, refined/, in-progress/backend/, in-progress/frontend/, testing/, and review/. For each unclaimed card (no [agent] in filename) in each stage, dispatch it to the correct agent by writing to the named pipes at .kanban/.pipes/. Use: echo 'CARD_PATH|STAGE' > .kanban/.pipes/AGENT. Routing rules: backlog→jules; refined with no architecture.component→jensen, refined with architecture+backend/core-logic label→james, refined with architecture+frontend/ui-component label→jony; in-progress/backend→james; in-progress/frontend→jony; testing→judge; review→jobs. Dispatch all stages that have waiting cards — do not stop after the first."
  else
    echo "[Joshua] Processing: $CARD_PATH"
    claude "Joshua, handle the task: $CARD_PATH (context: $STAGE). Read the card if it exists and act as tech lead to unblock or route it."
  fi

  echo "[Joshua] Done. Waiting..."
done
