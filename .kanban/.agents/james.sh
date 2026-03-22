#!/usr/bin/env bash
PIPE=".kanban/.pipes/james"
echo "[James] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[James] Processing: $CARD_PATH"
  claude "James, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the backend developer — implement using TDD, task by task, committing after each Red/Green/Refactor phase."
  echo "[James] Done. Waiting..."
done
