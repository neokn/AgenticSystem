// Package sessionstore provides a file-backed session.Service implementation
// that stores each session as a JSONL file (one event per line) with a JSON
// metadata sidecar.
//
// File layout under the configured directory:
//
//	<dir>/<session-id>.jsonl      — append-only event log
//	<dir>/<session-id>.meta.json  — session identity sidecar (for List queries)
//
// See docs/adr/0007-jsonl-session-store.md for design rationale.
package sessionstore

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/session"
)

// NewJSONLService returns a session.Service backed by JSONL files under dir.
// The directory is created if it does not exist.
func NewJSONLService(dir string) session.Service {
	return &jsonlService{dir: dir}
}

// sessionMeta is written to <id>.meta.json for fast List queries.
type sessionMeta struct {
	AppName   string    `json:"app_name"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

// jsonlService implements session.Service.
type jsonlService struct {
	dir string
}

// metaPath returns the path to the metadata sidecar for a session.
func (s *jsonlService) metaPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".meta.json")
}

// jsonlPath returns the path to the JSONL event log for a session.
func (s *jsonlService) jsonlPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".jsonl")
}

// Create implements session.Service.
func (s *jsonlService) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required, got app_name: %q, user_id: %q", req.AppName, req.UserID)
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	now := time.Now()

	// Write metadata sidecar.
	meta := sessionMeta{
		AppName:   req.AppName,
		UserID:    req.UserID,
		SessionID: sessionID,
		CreatedAt: now,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(s.metaPath(sessionID), metaBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write meta: %w", err)
	}

	// Create empty JSONL file.
	f, err := os.OpenFile(s.jsonlPath(sessionID), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create jsonl: %w", err)
	}
	f.Close()

	// Initialise state from CreateRequest.State, excluding temp keys.
	initialState := make(map[string]any)
	if req.State != nil {
		_, _, sessionDelta := extractStateDeltas(req.State)
		maps.Copy(initialState, sessionDelta)
	}

	sess := &jsonlSession{
		appName:   req.AppName,
		userID:    req.UserID,
		sessionID: sessionID,
		state:     initialState,
		updatedAt: now,
	}

	return &session.CreateResponse{Session: sess}, nil
}

// Get implements session.Service.
func (s *jsonlService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	appName, userID, sessionID := req.AppName, req.UserID, req.SessionID
	if appName == "" || userID == "" || sessionID == "" {
		return nil, fmt.Errorf("app_name, user_id, session_id are required")
	}

	// Read and validate metadata.
	metaBytes, err := os.ReadFile(s.metaPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		return nil, fmt.Errorf("read meta for %q: %w", sessionID, err)
	}
	var meta sessionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal meta for %q: %w", sessionID, err)
	}

	// Replay JSONL file.
	events, appDeltaAcc, userDeltaAcc, sessionState, updatedAt, err := s.replayJSONL(sessionID)
	if err != nil {
		return nil, err
	}

	// Merge accumulated state scopes.
	mergedState := mergeStates(appDeltaAcc, userDeltaAcc, sessionState)

	sess := &jsonlSession{
		appName:   meta.AppName,
		userID:    meta.UserID,
		sessionID: meta.SessionID,
		state:     mergedState,
		events:    events,
		updatedAt: updatedAt,
	}

	// Apply in-memory filters.
	filtered := events
	if req.NumRecentEvents > 0 {
		start := len(filtered) - req.NumRecentEvents
		if start < 0 {
			start = 0
		}
		filtered = filtered[start:]
	}
	if !req.After.IsZero() && len(filtered) > 0 {
		firstIdx := sort.Search(len(filtered), func(i int) bool {
			return !filtered[i].Timestamp.Before(req.After)
		})
		filtered = filtered[firstIdx:]
	}

	resultSess := &jsonlSession{
		appName:   sess.appName,
		userID:    sess.userID,
		sessionID: sess.sessionID,
		state:     maps.Clone(mergedState),
		events:    filtered,
		updatedAt: sess.updatedAt,
	}

	return &session.GetResponse{Session: resultSess}, nil
}

// replayJSONL reads all events from the JSONL file for sessionID.
// Corrupt lines are skipped with a slog.Warn.
func (s *jsonlService) replayJSONL(sessionID string) (
	events []*session.Event,
	appDelta, userDelta, sessionState map[string]any,
	updatedAt time.Time,
	err error,
) {
	appDelta = make(map[string]any)
	userDelta = make(map[string]any)
	sessionState = make(map[string]any)

	f, err := os.Open(s.jsonlPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, appDelta, userDelta, sessionState, time.Time{}, nil
		}
		return nil, nil, nil, nil, time.Time{}, fmt.Errorf("open jsonl for %q: %w", sessionID, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow large lines for events with base64 blobs etc.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev session.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Warn("sessionstore: skipping corrupt JSONL line",
				"session_id", sessionID,
				"line", lineNum,
				"raw", string(line),
				"error", err,
			)
			continue
		}

		// Accumulate state deltas.
		if len(ev.Actions.StateDelta) > 0 {
			ad, ud, sd := extractStateDeltas(ev.Actions.StateDelta)
			maps.Copy(appDelta, ad)
			maps.Copy(userDelta, ud)
			maps.Copy(sessionState, sd)
		}

		if !ev.Timestamp.IsZero() && ev.Timestamp.After(updatedAt) {
			updatedAt = ev.Timestamp
		}

		evCopy := ev
		events = append(events, &evCopy)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, nil, nil, time.Time{}, fmt.Errorf("scan jsonl for %q: %w", sessionID, err)
	}

	return events, appDelta, userDelta, sessionState, updatedAt, nil
}

// List implements session.Service.
func (s *jsonlService) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("app_name is required")
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &session.ListResponse{Sessions: nil}, nil
		}
		return nil, fmt.Errorf("read dir %q: %w", s.dir, err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}

		metaBytes, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			slog.Warn("sessionstore: skipping unreadable meta file",
				"file", entry.Name(), "error", err)
			continue
		}
		var meta sessionMeta
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			slog.Warn("sessionstore: skipping corrupt meta file",
				"file", entry.Name(), "error", err)
			continue
		}

		if meta.AppName != req.AppName {
			continue
		}
		if req.UserID != "" && meta.UserID != req.UserID {
			continue
		}

		sess := &jsonlSession{
			appName:   meta.AppName,
			userID:    meta.UserID,
			sessionID: meta.SessionID,
			state:     make(map[string]any),
			updatedAt: meta.CreatedAt,
		}
		sessions = append(sessions, sess)
	}

	return &session.ListResponse{Sessions: sessions}, nil
}

// Delete implements session.Service.
func (s *jsonlService) Delete(ctx context.Context, req *session.DeleteRequest) error {
	appName, userID, sessionID := req.AppName, req.UserID, req.SessionID
	if appName == "" || userID == "" || sessionID == "" {
		return fmt.Errorf("app_name, user_id, session_id are required")
	}

	var errs []string
	if err := os.Remove(s.jsonlPath(sessionID)); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err.Error())
	}
	if err := os.Remove(s.metaPath(sessionID)); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("delete session %q: %s", sessionID, strings.Join(errs, "; "))
	}
	return nil
}

// AppendEvent implements session.Service.
func (s *jsonlService) AppendEvent(ctx context.Context, curSession session.Session, event *session.Event) error {
	if curSession == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	// Skip partial events per ADK contract.
	if event.Partial {
		return nil
	}

	// Strip temp: keys before persisting.
	stripped := trimTempDeltaState(event)

	line, err := json.Marshal(stripped)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}

	// Open-write-close per call; O_APPEND guarantees atomic kernel-level append.
	f, err := os.OpenFile(s.jsonlPath(curSession.ID()), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open jsonl for append: %w", err)
	}
	_, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write event: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close jsonl: %w", closeErr)
	}
	return nil
}

// trimTempDeltaState removes temp: keys from the event's StateDelta.
// Returns a shallow copy of the event with a filtered StateDelta if needed.
func trimTempDeltaState(event *session.Event) *session.Event {
	if len(event.Actions.StateDelta) == 0 {
		return event
	}

	filtered := make(map[string]any)
	for k, v := range event.Actions.StateDelta {
		if !strings.HasPrefix(k, session.KeyPrefixTemp) {
			filtered[k] = v
		}
	}

	// Build a copy with filtered delta to avoid mutating the caller's event.
	copy := *event
	copy.Actions.StateDelta = filtered
	return &copy
}

// ---- jsonlSession: implements session.Session ----

type jsonlSession struct {
	appName   string
	userID    string
	sessionID string

	mu        sync.RWMutex
	events    []*session.Event
	state     map[string]any
	updatedAt time.Time
}

func (s *jsonlSession) ID() string      { return s.sessionID }
func (s *jsonlSession) AppName() string { return s.appName }
func (s *jsonlSession) UserID() string  { return s.userID }

func (s *jsonlSession) State() session.State {
	return &jsonlState{mu: &s.mu, state: s.state}
}

func (s *jsonlSession) Events() session.Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return jsonlEvents(s.events)
}

func (s *jsonlSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// ---- jsonlEvents: implements session.Events ----

type jsonlEvents []*session.Event

func (e jsonlEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e jsonlEvents) Len() int { return len(e) }

func (e jsonlEvents) At(i int) *session.Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

// ---- jsonlState: implements session.State ----

type jsonlState struct {
	mu    *sync.RWMutex
	state map[string]any
}

func (s *jsonlState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return val, nil
}

func (s *jsonlState) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
	return nil
}

func (s *jsonlState) All() iter.Seq2[string, any] {
	s.mu.RLock()
	copy := maps.Clone(s.state)
	s.mu.RUnlock()
	return func(yield func(string, any) bool) {
		for k, v := range copy {
			if !yield(k, v) {
				return
			}
		}
	}
}

// ---- State delta helpers (mirrors google.golang.org/adk/internal/sessionutils) ----
//
// These re-implement the internal sessionutils functions because the ADK
// sessionutils package is internal to the ADK module and cannot be imported.

const (
	appPrefix  = "app:"
	userPrefix = "user:"
)

// extractStateDeltas splits a state delta map into (appDelta, userDelta, sessionDelta).
// temp: keys are dropped. app: and user: prefixes are stripped from the output keys.
func extractStateDeltas(delta map[string]any) (appDelta, userDelta, sessionDelta map[string]any) {
	appDelta = make(map[string]any)
	userDelta = make(map[string]any)
	sessionDelta = make(map[string]any)
	for k, v := range delta {
		if clean, ok := strings.CutPrefix(k, appPrefix); ok {
			appDelta[clean] = v
		} else if clean, ok := strings.CutPrefix(k, userPrefix); ok {
			userDelta[clean] = v
		} else if !strings.HasPrefix(k, session.KeyPrefixTemp) {
			sessionDelta[k] = v
		}
	}
	return
}

// mergeStates rebuilds the combined state map from accumulated app/user/session deltas,
// re-adding the app: and user: prefixes.
func mergeStates(appState, userState, sessionState map[string]any) map[string]any {
	total := len(appState) + len(userState) + len(sessionState)
	merged := make(map[string]any, total)
	maps.Copy(merged, sessionState)
	for k, v := range appState {
		merged[appPrefix+k] = v
	}
	for k, v := range userState {
		merged[userPrefix+k] = v
	}
	return merged
}

// Compile-time interface checks.
var (
	_ session.Service = (*jsonlService)(nil)
	_ session.Session = (*jsonlSession)(nil)
	_ session.Events  = (jsonlEvents)(nil)
	_ session.State   = (*jsonlState)(nil)
)
