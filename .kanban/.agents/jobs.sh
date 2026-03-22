#!/usr/bin/env bash
PIPE=".kanban/.pipes/jobs"
echo "[Jobs] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[Jobs] Processing: $CARD_PATH"
  claude "Jobs, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the code reviewer — perform a structured review across all 8 dimensions."
  echo "[Jobs] Done. Waiting..."
done
