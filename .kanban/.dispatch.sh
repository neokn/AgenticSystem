#!/usr/bin/env bash
set -euo pipefail

CARD_PATH="${1:-}"
EVENT="${2:-}"

# Guard: only .yml files
[[ "$CARD_PATH" == *.yml ]] || exit 0

# Guard: ignore claimed cards (filename contains [)
BASENAME=$(basename "$CARD_PATH")
[[ "$BASENAME" != *"["* ]] || exit 0

# Extract stage from path (basename of parent dir)
STAGE=$(basename "$(dirname "$CARD_PATH")")

# in-progress subfolders: only dispatch on add events
if [[ "$STAGE" == "backend" || "$STAGE" == "frontend" ]]; then
  [[ "$EVENT" == "add" ]] || exit 0
else
  [[ "$EVENT" == "add" || "$EVENT" == "change" ]] || exit 0
fi

# Map stage to agent
case "$STAGE" in
  backlog)
    AGENT="jules"
    ;;
  refined)
    ARCH=$(yq '.architecture.component // ""' "$CARD_PATH" 2>/dev/null || echo "")
    if [[ -z "$ARCH" || "$ARCH" == "null" ]]; then
      AGENT="jensen"
    else
      LABELS=$(yq '.labels // [] | .[]' "$CARD_PATH" 2>/dev/null || echo "")
      if echo "$LABELS" | grep -qE '^(backend|core-logic)$'; then
        AGENT="james"
      else
        AGENT="jony"
      fi
    fi
    ;;
  backend)   AGENT="james"  ;;
  frontend)  AGENT="jony"   ;;
  testing)   AGENT="judge"  ;;
  review)    AGENT="jobs"   ;;
  *)         exit 0         ;;
esac

PIPE=".kanban/.pipes/$AGENT"

# Lock: prevent duplicate dispatch
LOCK_DIR=".kanban/.locks"
mkdir -p "$LOCK_DIR"
LOCK_FILE="$LOCK_DIR/$BASENAME.lock"
mkdir "$LOCK_FILE" 2>/dev/null || exit 0
trap 'rmdir "$LOCK_FILE" 2>/dev/null' EXIT

# Log
echo "[$(date -Iseconds)] → $AGENT: $CARD_PATH ($STAGE)" >> .kanban/.dispatch.log

# Write to agent pipe
echo "$CARD_PATH|$STAGE" > "$PIPE"
