#!/usr/bin/env bash
PIPE=".kanban/.pipes/jules"
echo "[Jules] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[Jules] Processing: $CARD_PATH"
  claude "Jules, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the requirements analyst and story refiner."
  echo "[Jules] Done. Waiting..."
done
