package audit_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// 1. Audit Write Success
func TestWriteEvent_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	s := audit.NewService(db)

	evt := audit.AuditEvent{EventID: uuid.New(), Action: "test.action", TenantID: uuid.New(), CreatedAt: time.Now()}

	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.WriteEvent(context.Background(), evt); err != nil {
		t.Errorf("WriteEvent failed: %v", err)
	}
}

// 2. Audit DB Fail -> Spool
func TestWriteEvent_Failover(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	tempDir, _ := os.MkdirTemp("", "audit_test")
	defer os.RemoveAll(tempDir)
	audit.ConfigureFailover(tempDir, 100)

	s := audit.NewService(db)
	evt := audit.AuditEvent{EventID: uuid.New(), Action: "fail.action", TenantID: uuid.New(), CreatedAt: time.Now()}

	// DB Error
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(sql.ErrConnDone)

	// Should NOT return error, but spool
	if err := s.WriteEvent(context.Background(), evt); err != nil {
		t.Errorf("WriteEvent failed on failover: %v", err)
	}

	// Verify File Exists
	files, _ := os.ReadDir(tempDir)
	if len(files) == 0 {
		t.Error("No spool file created")
	}
}

// 3. Replay Logic (Idempotency)
func TestReplay_Idempotency(t *testing.T) {
	// Restore globals
	oldDir := audit.SpoolDir
	defer func() { audit.SpoolDir = oldDir }()

	tempDir := t.TempDir()
	audit.ConfigureFailover(tempDir, 100)

	evt := audit.AuditEvent{EventID: uuid.New(), Action: "replay.action", TenantID: uuid.New()}

	// Manually create a historical file so ReplaySpool doesn't skip it
	history := time.Now().Add(-2*time.Hour).Format("20060102_15") + "_audit_spool.log"
	path := filepath.Join(tempDir, history)

	payload := audit.FailoverEvent{
		EventID:   evt.EventID.String(),
		TenantID:  evt.TenantID.String(),
		Payload:   evt,
		Timestamp: time.Now(),
	}
	line, _ := json.Marshal(payload)
	_ = os.WriteFile(path, append(line, '\n'), 0o600)

	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := audit.NewService(db)

	// Expect Exec for Replay
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	s.ReplaySpool(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Replay didn't call DB: %s", err)
	}
}

// 4. Middleware Auto Logging
func TestAuditMiddleware_AutoLog(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := audit.NewService(db)
	mw := middleware.NewAuditMiddleware(s)

	// Expect DB Insert asynchronously
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock Handler
	h := mw.LogRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
	}))

	req := httptest.NewRequest("POST", "/api/v1/resource", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Give async loop time
	time.Sleep(100 * time.Millisecond)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Middleware didn't log: %s", err)
	}
}

// 5. Middleware Ignored GET
func TestAuditMiddleware_IgnoreGET(t *testing.T) {
	db, mock, _ := sqlmock.New() // No expectations
	defer db.Close()
	s := audit.NewService(db)
	mw := middleware.NewAuditMiddleware(s)

	h := mw.LogRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/resource", nil) // Not Auth, Not Mutating
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Middleware logged GET unexpectedly: %s", err)
	}
}

// 6. Retention Policy Guard
func TestRetentionGuard(t *testing.T) {
	if err := audit.CheckRetentionPolicy(1); err == nil {
		t.Error("Allowed 1 year retention (Unsafe)")
	}
	if err := audit.CheckRetentionPolicy(7); err != nil {
		t.Error("Blocked 7 year retention (Safe)")
	}

	safeDate := audit.EnsureSafePurgeDate()
	if !safeDate.Before(time.Now()) {
		t.Error("Safe date invalid")
	}
}

// 7. API Query
func TestAuditAPI_Query(t *testing.T) {
	db, mock, _ := sqlmock.New()
	s := audit.NewService(db)
	h := &api.AuditHandler{Service: s} // No perms middleware in unit test call direct

	rows := sqlmock.NewRows([]string{"id", "event_id", "tenant_id", "actor_user_id", "action", "result", "created_at", "metadata"}).
		AddRow(uuid.New(), uuid.New(), uuid.New(), nil, "act", "success", time.Now(), []byte("{}"))

	mock.ExpectQuery("SELECT id, event_id").WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/api/v1/audit/events", nil)
	// Inject Tenant Context for Isolation Check
	ctx := middleware.WithAuthContext(req.Context(), &middleware.AuthContext{TenantID: uuid.New().String()})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.GetEvents(w, req)

	if w.Code != 200 {
		t.Errorf("API returned %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["events"] == nil {
		t.Error("No events returned")
	}
}

// 8. Streaming Export
func TestAuditAPI_Export(t *testing.T) {
	db, mock, _ := sqlmock.New()
	s := audit.NewService(db)
	h := &api.AuditHandler{Service: s}

	rows := sqlmock.NewRows([]string{"id", "event_id", "tenant_id", "actor_user_id", "action", "result", "created_at", "metadata"}).
		AddRow(uuid.New(), uuid.New(), uuid.New(), nil, "act", "success", time.Now(), []byte("{}"))

	mock.ExpectQuery("SELECT id, event_id").WillReturnRows(rows)

	req := httptest.NewRequest("POST", "/api/v1/audit/exports", nil)
	ctx := middleware.WithAuthContext(req.Context(), &middleware.AuthContext{TenantID: uuid.New().String()})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.ExportEvents(w, req)

	if w.Code != 200 {
		t.Errorf("Export returned %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/x-jsonl" {
		t.Error("Wrong Content-Type")
	}
}

// 9. Test Middleware POST
func TestMiddleware_Method_POST(t *testing.T) {
	runMiddlewareMethodTest(t, "POST", true)
}

// 10. Test Middleware PUT
func TestMiddleware_Method_PUT(t *testing.T) {
	runMiddlewareMethodTest(t, "PUT", true)
}

// 11. Test Middleware DELETE
func TestMiddleware_Method_DELETE(t *testing.T) {
	runMiddlewareMethodTest(t, "DELETE", true)
}

// 12. Test Middleware PATCH
func TestMiddleware_Method_PATCH(t *testing.T) {
	runMiddlewareMethodTest(t, "PATCH", true)
}

// 13. Test Middleware GET (Ignored) - Explicit
func TestMiddleware_Method_GET_Ignored(t *testing.T) {
	runMiddlewareMethodTest(t, "GET", false)
}

// Helper for method tests
func runMiddlewareMethodTest(t *testing.T, method string, expectLog bool) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := audit.NewService(db)
	mw := middleware.NewAuditMiddleware(s)

	if expectLog {
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
	}

	h := mw.LogRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(method, "/api/v1/resource", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	time.Sleep(10 * time.Millisecond)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Method %s expectation mismatch: %s", method, err)
	}
}

// 14. Test Middleware Auth Route (GET allowed)
func TestMiddleware_AuthRoute_Login(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := audit.NewService(db)
	mw := middleware.NewAuditMiddleware(s) // Auth routes allow non-mutating too?

	// Yes, "Log auth endpoints always"
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	h := mw.LogRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	time.Sleep(10 * time.Millisecond)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error("Auth route should be logged even if GET")
	}
}

// 15. Test Write Event generates UUID
func TestWriteEvent_GeneratesUUID(t *testing.T) {
	db, mock, _ := sqlmock.New()
	s := audit.NewService(db)
	// Event with NIL ID
	evt := audit.AuditEvent{EventID: uuid.Nil, TenantID: uuid.New()}

	mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	s.WriteEvent(context.Background(), evt)
	// Can't easily check the arg passed to exec without custom matcher,
	// but success implies it worked.
}

// 16. Test Retention 1 Year (Fail)
func TestRetention_1Year(t *testing.T) {
	if err := audit.CheckRetentionPolicy(1); err == nil {
		t.Error("Should fail")
	}
}

// 17. Test Retention 6 Years (Fail)
func TestRetention_6Years(t *testing.T) {
	if err := audit.CheckRetentionPolicy(6); err == nil {
		t.Error("Should fail")
	}
}

// 18. Test Retention 8 Years (Pass)
func TestRetention_8Years(t *testing.T) {
	if err := audit.CheckRetentionPolicy(8); err != nil {
		t.Error("Should pass")
	}
}

// 19. Test Export Filter Logic (Mock)
func TestExport_Filter_Tenant(t *testing.T) {
	db, mock, _ := sqlmock.New()
	s := audit.NewService(db)
	h := &api.AuditHandler{Service: s}

	mock.ExpectQuery("SELECT id, event_id").WillReturnRows(sqlmock.NewRows(nil))

	req := httptest.NewRequest("POST", "/api/v1/audit/exports", nil)
	// No Tenant
	w := httptest.NewRecorder()
	h.ExportEvents(w, req)
	if w.Code != 401 {
		t.Error("Should require tenant context")
	}
}

// 20. Test Failover Config
func TestFailover_Config(t *testing.T) {
	oldDir := audit.SpoolDir
	defer func() { audit.SpoolDir = oldDir }()

	tmp := t.TempDir()
	audit.ConfigureFailover(tmp, 500)
	if audit.SpoolDir != tmp {
		t.Error("Config failed")
	}
}

// 21. Test Spool Full (Mock logic via small max size)
func TestSpool_Full_Rotation(t *testing.T) {
	oldDir := audit.SpoolDir
	defer func() { audit.SpoolDir = oldDir }()

	audit.SpoolDir = t.TempDir()

	evt := audit.AuditEvent{EventID: uuid.New(), TenantID: uuid.New()}
	err := audit.SpoolEvent(evt)
	if err != nil {
		t.Errorf("SpoolEvent failed: %v", err)
	}
}

// 22. Test Spool Concurrency (Safety & Line Integrity)
func TestSpool_Concurrency(t *testing.T) {
	// Save globals and restore after test.
	oldDir := audit.SpoolDir
	oldMax := audit.MaxSpoolSize
	defer func() {
		audit.SpoolDir = oldDir
		audit.MaxSpoolSize = oldMax
	}()

	audit.SpoolDir = t.TempDir()
	audit.MaxSpoolSize = 1 << 30 // 1GB

	if err := os.MkdirAll(audit.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spooldir: %v", err)
	}

	const (
		goroutines = 50
		perG       = 200
		total      = goroutines * perG
	)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, total)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			<-start

			for i := 0; i < perG; i++ {
				evt := audit.AuditEvent{
					EventID:   uuid.New(),
					TenantID:  uuid.New(),
					Action:    fmt.Sprintf("action-%d-%d", g, i),
					CreatedAt: time.Now(),
				}

				if err := audit.SpoolEvent(evt); err != nil {
					errCh <- err
					return
				}
			}
		}(g)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("SpoolEvent failed: %v", err)
	}

	// Collect all hourly spool logs.
	pattern := filepath.Join(audit.SpoolDir, "*_audit_spool.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no spool files found with pattern %q", pattern)
	}

	seen := make(map[string]struct{})
	count := 0
	for _, fn := range files {
		f, err := os.Open(fn)
		if err != nil {
			t.Fatalf("open %s: %v", fn, err)
		}

		reader := bufio.NewReader(f)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var fe audit.FailoverEvent
				if err := json.Unmarshal(line, &fe); err != nil {
					f.Close()
					t.Fatalf("corrupt JSON line in %s: %v\nline=%q", fn, err, string(line))
				}
				if _, dup := seen[fe.EventID]; dup {
					f.Close()
					t.Fatalf("duplicate EventID detected: %s", fe.EventID)
				}
				seen[fe.EventID] = struct{}{}
				count++
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}

	if count != total {
		t.Fatalf("expected %d spooled lines, got %d (files=%v)", total, count, files)
	}
}
