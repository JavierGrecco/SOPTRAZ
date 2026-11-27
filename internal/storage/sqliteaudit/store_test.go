package sqliteaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/audit"
)

const testInterventionID = "2aef4e6d-9d50-4cb6-9c96-22a26abf1587"

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func sampleDraft(eventType string, payload string) audit.Draft {
	return audit.Draft{
		InterventionID: testInterventionID,
		EventType:      eventType,
		OccurredAt:     time.Date(2026, 11, 27, 19, 0, 0, 0, time.UTC),
		Actor:          audit.Actor{Type: "user", ActorID: "technician-demo-001"},
		Payload:        json.RawMessage(payload),
	}
}

func TestAppendBuildsAndVerifiesHashChain(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first, err := store.Append(ctx, sampleDraft("intervention.created", `{"source":"field","category":"network"}`))
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if first.Sequence != 1 || first.PreviousHash != "" || len(first.Hash) != 64 {
		t.Fatalf("unexpected first event chain metadata: %#v", first)
	}

	second, err := store.Append(ctx, sampleDraft("diagnostic.completed", `{"findings":2}`))
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	if second.Sequence != 2 || second.PreviousHash != first.Hash {
		t.Fatalf("second event does not link to first: %#v", second)
	}
	if err := store.Verify(ctx, testInterventionID); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	events, err := store.List(ctx, testInterventionID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 2 || events[0].Hash != first.Hash || events[1].Hash != second.Hash {
		t.Fatalf("unexpected ordered events: %#v", events)
	}
}

func TestAppendIsSerializedForConcurrentWriters(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := store.Append(ctx, sampleDraft("operation.completed", fmt.Sprintf("{\"index\":%d}", index)))
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Append() error = %v", err)
	}

	events, err := store.List(ctx, testInterventionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers {
		t.Fatalf("event count = %d, want %d", len(events), writers)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event at index %d has sequence %d", index, event.Sequence)
		}
	}
	if err := store.Verify(ctx, testInterventionID); err != nil {
		t.Fatalf("Verify() after concurrent writes: %v", err)
	}
}

func TestDatabaseBlocksEventMutationAndDeletion(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	event, err := store.Append(ctx, sampleDraft("intervention.created", `{"source":"field"}`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx,
		"UPDATE audit_events SET event_json = '{}' WHERE event_id = ?",
		event.EventID,
	); err == nil {
		t.Fatal("UPDATE should be blocked by append-only trigger")
	}
	if _, err := store.db.ExecContext(ctx,
		"DELETE FROM audit_events WHERE event_id = ?",
		event.EventID,
	); err == nil {
		t.Fatal("DELETE should be blocked by append-only trigger")
	}
	if _, err := store.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO audit_events(event_id, intervention_id, sequence, event_json, hash) VALUES (?, ?, ?, ?, ?)",
		event.EventID,
		event.InterventionID,
		event.Sequence,
		"{}",
		event.Hash,
	); err == nil {
		t.Fatal("INSERT OR REPLACE should be blocked by the recursive append-only delete trigger")
	}
	if err := store.Verify(ctx, testInterventionID); err != nil {
		t.Fatalf("immutable event changed unexpectedly: %v", err)
	}
}

func TestVerifyDetectsAlteredEventWhenTriggerIsBypassed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	event, err := store.Append(ctx, sampleDraft("intervention.created", `{"category":"network"}`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER audit_events_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE audit_events SET event_json = replace(event_json, '\"network\"', '\"tampered\"') WHERE event_id = ?",
		event.EventID,
	); err != nil {
		t.Fatal(err)
	}

	err = store.Verify(ctx, testInterventionID)
	if !errors.Is(err, ErrCorruptChain) {
		t.Fatalf("Verify() error = %v, want ErrCorruptChain", err)
	}
}

func TestAppendRejectsInvalidEventAndPayload(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	invalidType := sampleDraft("arbitrary.command", `{"ok":true}`)
	if _, err := store.Append(ctx, invalidType); err == nil {
		t.Fatal("unknown event type must be rejected by the event contract")
	}

	invalidPayload := sampleDraft("intervention.created", `["not","an","object"]`)
	if _, err := store.Append(ctx, invalidPayload); err == nil {
		t.Fatal("non-object event payload must be rejected")
	}
}

func TestVerifyDetectsHeadMismatchAfterTailDeletion(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	_, err := store.Append(ctx, sampleDraft("intervention.created", `{"step":1}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Append(ctx, sampleDraft("diagnostic.completed", `{"step":2}`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER audit_events_no_delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx,
		"DELETE FROM audit_events WHERE intervention_id = ? AND sequence = 2",
		testInterventionID,
	); err != nil {
		t.Fatal(err)
	}

	err = store.Verify(ctx, testInterventionID)
	if !errors.Is(err, ErrCorruptChain) && !errors.Is(err, ErrHeadMismatch) {
		t.Fatalf("Verify() error = %v, want chain/head mismatch", err)
	}
}

func TestEventsPersistAcrossDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audit.sqlite")

	firstStore, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstStore.Append(ctx, sampleDraft("intervention.created", "{\"source\":\"field\"}"))
	if err != nil {
		_ = firstStore.Close()
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if err := reopened.Verify(ctx, testInterventionID); err != nil {
		t.Fatalf("Verify() after reopen: %v", err)
	}
	events, err := reopened.List(ctx, testInterventionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID != first.EventID {
		t.Fatalf("unexpected persisted events after reopen: %#v", events)
	}
}

func TestListDetectsMismatchBetweenEventJSONAndIndexedHash(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	event, err := store.Append(ctx, sampleDraft("intervention.created", "{\"source\":\"field\"}"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER audit_events_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE audit_events SET hash = ? WHERE event_id = ?",
		strings.Repeat("0", 64),
		event.EventID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, testInterventionID); !errors.Is(err, ErrCorruptChain) {
		t.Fatalf("List() error = %v, want ErrCorruptChain", err)
	}
}

func TestSQLiteURIPath(t *testing.T) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("filepath.Abs(.) error = %v", err)
	}

	cases := []struct {
		name    string
		path    string
		windows bool
		want    string
	}{
		{
			name:    "Windows drive path",
			path:    "E:\\Shared\\Workspaces\\SOPTRAZ\\audit.sqlite",
			windows: true,
			want:    "/E:/Shared/Workspaces/SOPTRAZ/audit.sqlite",
		},
		{
			name:    "Windows drive path with forward slashes",
			path:    "c:/repo/audit.sqlite",
			windows: true,
			want:    "/c:/repo/audit.sqlite",
		},
		{
			name:    "Windows UNC path",
			path:    "\\\\server\\share\\repo\\audit.sqlite",
			windows: true,
			want:    "//server/share/repo/audit.sqlite",
		},
		{
			name:    "POSIX absolute path",
			path:    "/home/tech/SOPTRAZ/audit.sqlite",
			windows: false,
			want:    "/home/tech/SOPTRAZ/audit.sqlite",
		},
		{
			name:    "POSIX path with URI control characters",
			path:    "/home/tech/odd?dir#fragment/audit.sqlite",
			windows: false,
			want:    "/home/tech/odd?dir#fragment/audit.sqlite",
		},
		{
			name:    "relative POSIX path becomes absolute",
			path:    filepath.Join(".beads", "audit.sqlite"),
			windows: false,
			want:    filepath.Join(cwd, ".beads", "audit.sqlite"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteURIPath(tc.path, tc.windows); got != tc.want {
				t.Fatalf("sqliteURIPath(%q, windows=%v) = %q, want %q", tc.path, tc.windows, got, tc.want)
			}
		})
	}
}

func TestSQLiteFileDSNWindowsDrivePathHasEmptyAuthority(t *testing.T) {
	dsn := sqliteFileDSN("E:\\Shared\\Workspaces\\SOPTRAZ\\audit.sqlite", true)
	want := "file:///E:/Shared/Workspaces/SOPTRAZ/audit.sqlite?_txlock=immediate"
	if dsn != want {
		t.Fatalf("DSN = %q, want %q", dsn, want)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Host != "" {
		t.Fatalf("URI authority = %q, want empty", parsed.Host)
	}
	if strings.Contains(dsn, "%5C") {
		t.Fatalf("DSN contains escaped Windows separators: %q", dsn)
	}
}

func TestSQLiteFileDSNWindowsUNCPathHasEmptyAuthority(t *testing.T) {
	dsn := sqliteFileDSN("\\\\server\\share\\repo\\audit.sqlite", true)
	want := "file:////server/share/repo/audit.sqlite?_txlock=immediate"
	if dsn != want {
		t.Fatalf("UNC DSN = %q, want %q", dsn, want)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Host != "" || parsed.Path != "//server/share/repo/audit.sqlite" {
		t.Fatalf("UNC URI must have empty authority and rooted path: host=%q path=%q", parsed.Host, parsed.Path)
	}
}

func TestSQLiteFileDSNEncodesReservedPathCharacters(t *testing.T) {
	path := "/tmp/SOPTRAZ/odd ?dir#fragment/audit.sqlite"
	dsn := sqliteFileDSN(path, false)
	for _, escaped := range []string{"%20", "%3F", "%23"} {
		if !strings.Contains(dsn, escaped) {
			t.Fatalf("DSN %q does not escape %q", dsn, escaped)
		}
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Host != "" || parsed.Path != path || parsed.Fragment != "" {
		t.Fatalf("reserved path characters were not preserved: host=%q path=%q fragment=%q", parsed.Host, parsed.Path, parsed.Fragment)
	}
	if parsed.Query().Get("_txlock") != "immediate" {
		t.Fatalf("txlock = %q, want immediate", parsed.Query().Get("_txlock"))
	}
}

func TestAppendRejectsTamperedHeadBeforeWriting(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, err := store.Append(ctx, sampleDraft("intervention.created", "{\"step\":1}")); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx,
		"UPDATE audit_heads SET head_hash = ? WHERE intervention_id = ?",
		strings.Repeat("0", 64),
		testInterventionID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Append(ctx, sampleDraft("diagnostic.completed", "{\"step\":2}")); !errors.Is(err, ErrHeadMismatch) {
		t.Fatalf("Append() error = %v, want ErrHeadMismatch", err)
	}
}

func TestSQLiteDSNRootsRelativePaths(t *testing.T) {
	dsn := sqliteDSN(filepath.Join(".beads", "audit.sqlite"))
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Host != "" || !strings.HasPrefix(dsn, "file:///") {
		t.Fatalf("relative path DSN must have an empty authority: %q", dsn)
	}
	if parsed.Query().Get("_txlock") != "immediate" {
		t.Fatalf("txlock = %q, want immediate", parsed.Query().Get("_txlock"))
	}
}

func TestOpenAppendsToRelativeDatabasePath(t *testing.T) {
	t.Chdir(t.TempDir())

	store, err := Open(context.Background(), "audit.sqlite")
	if err != nil {
		t.Fatalf("Open(relative path) error = %v", err)
	}
	defer store.Close()

	event, err := store.Append(context.Background(), sampleDraft("intervention.created", "{\"source\":\"relative-path-test\"}"))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := store.Verify(context.Background(), event.InterventionID); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	_, err := Open(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "vacía") {
		t.Fatalf("Open(empty) error = %v, want empty-path error", err)
	}
}
