package jsonl_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/neokn/agenticsystem/internal/infra/persistence/jsonl"
)

// newTempDir creates a temporary directory for test session files.
func newTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sessionstore_test_*")
	if err != nil {
		t.Fatalf("newTempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// newService creates a JSONLService backed by a temp directory.
func newService(t *testing.T) session.Service {
	t.Helper()
	return jsonl.NewJSONLService(newTempDir(t))
}

// createSession is a test helper that creates a session and returns it.
func createSession(t *testing.T, svc session.Service, appName, userID, sessionID string) session.Session {
	t.Helper()
	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return resp.Session
}

// makeTextEvent creates a simple text event for testing.
func makeTextEvent(author, text string) *session.Event {
	e := session.NewEvent("inv-1")
	e.Author = author
	e.Content = &genai.Content{
		Parts: []*genai.Part{{Text: text}},
		Role:  "model",
	}
	return e
}

// ---- Tests ----

func TestCreate_should_succeed_when_app_name_and_user_id_provided(t *testing.T) {
	svc := newService(t)

	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Session.ID() != "sess1" {
		t.Errorf("want ID=sess1, got %s", resp.Session.ID())
	}
	if resp.Session.AppName() != "myapp" {
		t.Errorf("want AppName=myapp, got %s", resp.Session.AppName())
	}
	if resp.Session.UserID() != "user1" {
		t.Errorf("want UserID=user1, got %s", resp.Session.UserID())
	}
}

func TestCreate_should_auto_generate_session_id_when_not_provided(t *testing.T) {
	svc := newService(t)

	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "myapp",
		UserID:  "user1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Session.ID() == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestCreate_should_error_when_app_name_empty(t *testing.T) {
	svc := newService(t)

	_, err := svc.Create(context.Background(), &session.CreateRequest{
		UserID: "user1",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreate_should_error_when_user_id_empty(t *testing.T) {
	svc := newService(t)

	_, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "myapp",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreate_should_persist_meta_file(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)

	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	metaPath := filepath.Join(dir, resp.Session.ID()+".meta.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Errorf("expected meta file at %s", metaPath)
	}
}

func TestCreate_should_persist_empty_jsonl_file(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)

	resp, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	jsonlPath := filepath.Join(dir, resp.Session.ID()+".jsonl")
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		t.Errorf("expected jsonl file at %s", jsonlPath)
	}
}

func TestGet_should_error_when_session_not_found(t *testing.T) {
	svc := newService(t)

	_, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "nonexistent",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected error message to contain session ID %q, got: %v", "nonexistent", err)
	}
}

func TestGet_should_return_session_after_create(t *testing.T) {
	svc := newService(t)
	createSession(t, svc, "myapp", "user1", "sess1")

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})

	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Session.ID() != "sess1" {
		t.Errorf("want sess1, got %s", resp.Session.ID())
	}
}

func TestAppendEvent_should_persist_and_replay_events(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	ev1 := makeTextEvent("user", "hello")
	ev2 := makeTextEvent("agent", "hi there")

	if err := svc.AppendEvent(context.Background(), sess, ev1); err != nil {
		t.Fatalf("AppendEvent(ev1): %v", err)
	}
	if err := svc.AppendEvent(context.Background(), sess, ev2); err != nil {
		t.Fatalf("AppendEvent(ev2): %v", err)
	}

	// Simulate restart: create a new service pointing to the same directory
	svc2 := jsonl.NewJSONLService(dir)

	resp, err := svc2.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}

	if resp.Session.Events().Len() != 2 {
		t.Errorf("want 2 events, got %d", resp.Session.Events().Len())
	}

	ev := resp.Session.Events().At(0)
	if ev.Content == nil || len(ev.Content.Parts) == 0 || ev.Content.Parts[0].Text != "hello" {
		t.Errorf("want text=hello, got %v", ev.Content)
	}
}

func TestAppendEvent_should_skip_partial_events(t *testing.T) {
	svc := newService(t)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	partial := makeTextEvent("agent", "streaming...")
	partial.Partial = true

	if err := svc.AppendEvent(context.Background(), sess, partial); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Session.Events().Len() != 0 {
		t.Errorf("want 0 events (partial skipped), got %d", resp.Session.Events().Len())
	}
}

func TestGet_should_skip_corrupt_last_line_and_log_warn(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	ev := makeTextEvent("user", "good event")
	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	// Corrupt the last line by appending truncated JSON
	jsonlPath := filepath.Join(dir, "sess1.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	_, _ = f.WriteString(`{"corrupt":true,"unfinished"` + "\n")
	f.Close()

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Get should return nil error even with corrupt line: %v", err)
	}
	// Only the good event survives
	if resp.Session.Events().Len() != 1 {
		t.Errorf("want 1 event, got %d", resp.Session.Events().Len())
	}
}

func TestAppendEvent_concurrent_writes_should_preserve_both_events(t *testing.T) {
	svc := newService(t)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ev := makeTextEvent("user", "message-A")
		if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
			t.Errorf("AppendEvent A: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		ev := makeTextEvent("agent", "message-B")
		if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
			t.Errorf("AppendEvent B: %v", err)
		}
	}()

	wg.Wait()

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Session.Events().Len() != 2 {
		t.Errorf("want 2 events after concurrent writes, got %d", resp.Session.Events().Len())
	}
}

func TestList_should_return_sessions_matching_app_and_user(t *testing.T) {
	svc := newService(t)
	createSession(t, svc, "app1", "user1", "s1")
	createSession(t, svc, "app1", "user1", "s2")
	createSession(t, svc, "app1", "user2", "s3")
	createSession(t, svc, "app2", "user1", "s4")

	resp, err := svc.List(context.Background(), &session.ListRequest{
		AppName: "app1",
		UserID:  "user1",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Errorf("want 2 sessions for app1/user1, got %d", len(resp.Sessions))
	}
}

func TestDelete_should_remove_session_files(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)
	createSession(t, svc, "myapp", "user1", "sess1")

	err := svc.Delete(context.Background(), &session.DeleteRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Both files should be gone
	metaPath := filepath.Join(dir, "sess1.meta.json")
	jsonlPath := filepath.Join(dir, "sess1.jsonl")
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("expected meta file to be deleted")
	}
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Error("expected jsonl file to be deleted")
	}
}

func TestGet_should_apply_num_recent_events_filter(t *testing.T) {
	svc := newService(t)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	for i := range 5 {
		ev := makeTextEvent("user", string(rune('a'+i)))
		_ = svc.AppendEvent(context.Background(), sess, ev)
	}

	resp, err := svc.Get(context.Background(), &session.GetRequest{
		AppName:         "myapp",
		UserID:          "user1",
		SessionID:       "sess1",
		NumRecentEvents: 3,
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Session.Events().Len() != 3 {
		t.Errorf("want 3 events with NumRecentEvents=3, got %d", resp.Session.Events().Len())
	}
}

func TestAppendEvent_should_store_full_event_but_replay_only_content(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	ev := makeTextEvent("agent", "hello world")

	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	// Raw JSONL should contain the full event (Author, Content, etc.).
	raw, err := os.ReadFile(filepath.Join(dir, "sess1.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.Contains(line, `"Content"`) {
		t.Error("JSONL should contain Content field")
	}
	if !strings.Contains(line, `"Author"`) {
		t.Error("JSONL should contain Author field (full event)")
	}

	// But replay should only reconstruct Content (role + parts).
	svc2 := jsonl.NewJSONLService(dir)
	resp, err := svc2.Get(context.Background(), &session.GetRequest{
		AppName: "myapp", UserID: "user1", SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	replayed := resp.Session.Events().At(0)
	if replayed.Content == nil || replayed.Content.Parts[0].Text != "hello world" {
		t.Errorf("replayed Content mismatch: %v", replayed.Content)
	}
	if replayed.Author != "agent" {
		t.Errorf("replayed Author should be 'agent', got %q", replayed.Author)
	}
}

func TestAppendEvent_should_strip_thought_signatures(t *testing.T) {
	dir := newTempDir(t)
	svc := jsonl.NewJSONLService(dir)
	sess := createSession(t, svc, "myapp", "user1", "sess1")

	ev := makeTextEvent("agent", "thinking reply")
	ev.Content.Parts = append(ev.Content.Parts, &genai.Part{
		Thought:          true,
		ThoughtSignature: []byte("opaque-sig-bytes"),
	})

	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "sess1.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	if strings.Contains(line, "thoughtSignature") {
		t.Error("JSONL should not contain thoughtSignature")
	}
	if !strings.Contains(line, "thinking reply") {
		t.Error("JSONL should still contain text content")
	}
}
