// Package jsonl provides a file-backed session.Service implementation
// that stores each session as a JSONL file (one event per line) with a JSON
// metadata sidecar.
//
// File layout under the configured directory:
//
//	<dir>/<session-id>.jsonl      — append-only event log
//	<dir>/<session-id>.meta.json  — session identity sidecar (for List queries)
//
// See docs/adr/0007-jsonl-session-store.md for design rationale.
package jsonl

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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
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


// stripThoughtSignatures returns a shallow copy of content with
// ThoughtSignature cleared from every Part. The original is not mutated.
func stripThoughtSignatures(c *genai.Content) *genai.Content {
	if c == nil {
		return nil
	}
	out := *c
	out.Parts = make([]*genai.Part, len(c.Parts))
	for i, p := range c.Parts {
		if len(p.ThoughtSignature) == 0 {
			out.Parts[i] = p
			continue
		}
		cp := *p
		cp.ThoughtSignature = nil
		out.Parts[i] = &cp
	}
	return &out
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

	// Idempotent: if session already exists, return without re-creating.
	if _, err := os.Stat(s.metaPath(sessionID)); err == nil {
		return &session.CreateResponse{Session: &jsonlSession{
			appName:   req.AppName,
			userID:    req.UserID,
			sessionID: sessionID,
			state:     make(map[string]any),
		}}, nil
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

	sess := &jsonlSession{
		appName:   req.AppName,
		userID:    req.UserID,
		sessionID: sessionID,
		state:     make(map[string]any),
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
	events, err := s.replayJSONL(sessionID)
	if err != nil {
		return nil, err
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

	sess := &jsonlSession{
		appName:   meta.AppName,
		userID:    meta.UserID,
		sessionID: meta.SessionID,
		state:     make(map[string]any),
		events:    filtered,
	}

	return &session.GetResponse{Session: sess}, nil
}

// replayJSONL reads all events from the JSONL file for sessionID and
// deserializes them as-is. Corrupt lines are skipped with a slog.Warn.
func (s *jsonlService) replayJSONL(sessionID string) ([]*session.Event, error) {
	f, err := os.Open(s.jsonlPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open jsonl for %q: %w", sessionID, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var events []*session.Event
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
				"error", err,
			)
			continue
		}

		events = append(events, &ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl for %q: %w", sessionID, err)
	}

	return events, nil
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

	// Update the in-memory session so the runner sees new events immediately.
	if sess, ok := curSession.(*jsonlSession); ok {
		sess.mu.Lock()
		sess.events = append(sess.events, event)
		sess.updatedAt = event.Timestamp
		sess.mu.Unlock()
	}

	stored := *event
	stored.Content = stripThoughtSignatures(event.Content)
	line, err := json.Marshal(&stored)
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


// Compile-time interface checks.
var (
	_ session.Service = (*jsonlService)(nil)
	_ session.Session = (*jsonlSession)(nil)
	_ session.Events  = (jsonlEvents)(nil)
	_ session.State   = (*jsonlState)(nil)
)
