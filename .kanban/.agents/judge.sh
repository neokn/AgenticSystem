#!/usr/bin/env bash
PIPE=".kanban/.pipes/judge"
echo "[Judge] Ready — waiting for cards"
while true; do
  IFS= read -r LINE < "$PIPE"
  CARD_PATH=$(echo "$LINE" | cut -d'|' -f1)
  STAGE=$(echo "$LINE" | cut -d'|' -f2)
  echo "[Judge] Processing: $CARD_PATH"
  claude "Judge, process the card at $CARD_PATH (stage: $STAGE). Read the card and complete your role as the test engineer — verify every acceptance criterion has a passing test, run the full suite."
  echo "[Judge] Done. Waiting..."
done
