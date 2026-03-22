#!/usr/bin/env bash
PIPE=".kanban/.pipes/jony"
echo "[Jony] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[Jony] Processing: $CARD_PATH"
  claude "Jony, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the frontend developer — implement using Atomic Design and TDD, task by task, committing after each Red/Green/Refactor phase."
  echo "[Jony] Done. Waiting..."
done
