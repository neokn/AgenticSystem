#!/usr/bin/env bash
PIPE=".kanban/.pipes/jensen"
echo "[Jensen] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[Jensen] Processing: $CARD_PATH"
  claude "Jensen, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the architect — add component, layer, pattern, and boundary decisions."
  echo "[Jensen] Done. Waiting..."
done
