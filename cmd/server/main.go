package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"

	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/license"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/platform/windows"
	"github.com/technosupport/ts-vms/internal/ratelimit"
	"github.com/technosupport/ts-vms/internal/session"
	"github.com/technosupport/ts-vms/internal/tokens"
	"github.com/technosupport/ts-vms/internal/users"
)

const (
	serviceName  = "TS-VMS-Control"
	eventIDStart = 100
	eventIDStop  = 101
	eventIDError = 102
)

func main() {
	// 1. Windows Service Check
	isService := windows.IsWindowsService()
	elog := windows.NewEventLogger(serviceName)
	defer elog.Close()

	if isService {
		elog.Info(eventIDStart, "Starting as Windows Service")
	}

	stopChan := make(chan struct{})
	if isService {
		go func() {
			if err := windows.RunAsService(serviceName, stopChan); err != nil {
				elog.Error(eventIDError, fmt.Sprintf("Service run error: %v", err))
				os.Exit(1)
			}
		}()
	}

	// 2. Config
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	redisAddr := os.Getenv("REDIS_ADDR")
	jwtKey := os.Getenv("JWT_SIGNING_KEY")

	if jwtKey == "" {
		jwtKey = "dev-secret-do-not-use-in-prod"
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPass, dbHost, dbName)

	// 2. DB Init
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("DB open error: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("DB ping error: %v", err)
	}

	// 3. Components
	// Shared Redis Client
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	// Managers
	sessionMgr := session.NewManager(redisAddr, "") // TODO: Update SessionMgr to use shared client in future refactor
	tokenMgr := tokens.NewManager(jwtKey)

	// Audit Service (Phase 1.5)
	auditService := audit.NewService(db)

	// Config Spooler (Using default from task or env helper later)
	// For now using hardcoded default from prompt requirements via ConfigureFailover
	audit.ConfigureFailover("C:\\ProgramData\\TechnoSupport\\VMS\\audit_spool", 1024)
	auditService.StartReplayer(context.Background()) // Background context for long running

	// License Manager (Phase 1.6)
	// Config Loading - Quick inline for Phase 1.6
	var licCfg struct {
		License struct {
			Path          string `yaml:"path"`
			PublicKeyPath string `yaml:"public_key_path"`
		} `yaml:"license"`
	}
	// Re-read config (inefficient but safe for this phase wiring)
	licCfgData, _ := os.ReadFile("config/default.yaml")
	_ = yaml.Unmarshal(licCfgData, &licCfg)

	// 1. Create Parser
	licenseParser, err := license.NewParser(licCfg.License.PublicKeyPath)
	if err != nil {
		log.Printf("Warning: Failed to load License Public Key: %v. License verification will fail.", err)
	}

	// 2. Create Manager
	usageStub := &license.StubUsageProvider{}
	licenseManager := license.NewManager(licCfg.License.Path, licenseParser, usageStub, auditService)

	// 3. Start Watcher & Scheduler
	licenseManager.StartWatcher(context.Background())
	licenseScheduler := license.NewScheduler(licenseManager)
	licenseScheduler.Start(context.Background())

	// RBAC Components
	blacklist := auth.NewRedisBlacklist(rdb)
	permModel := data.PermissionModel{DB: db}
	camResolver := middleware.StubCameraResolver{}

	// Helper to load Config (Quick inline for phase 1.4)
	var rootCfg struct {
		RateLimit middleware.Config `yaml:"rate_limit"`
	}
	cfgData, _ := os.ReadFile("config/default.yaml")
	_ = yaml.Unmarshal(cfgData, &rootCfg) // Error handling ignored for brevity in main

	limiter := ratelimit.NewLimiter(rdb, "stable-salt-val") // In prod use Env Var

	permsMiddleware := middleware.NewPermissionMiddleware(permModel, camResolver)
	jwtMiddleware := middleware.NewJWTAuth(tokenMgr, blacklist)

	// Rate Limit Middleware
	rlMiddleware := middleware.NewRateLimitMiddleware(limiter, tokenMgr, rootCfg.RateLimit, rootCfg.RateLimit.Endpoints)

	// Audit Middleware
	auditMiddleware := middleware.NewAuditMiddleware(auditService)

	authHandler := &api.AuthHandler{
		DB:      db,
		Tokens:  tokenMgr,
		Session: sessionMgr,
		Hasher:  auth.DefaultParams,
	}

	// Audit API Handler
	auditHandler := &api.AuditHandler{
		Service: auditService,
		Perms:   permsMiddleware,
	}

	// User Service (Phase 1.7)
	userRepo := data.UserModel{DB: db}
	userService := users.NewService(&userRepo, auditService, sessionMgr, tokenMgr)

	userHandler := &api.UserHandler{
		Service: userService,
	}

	// License API Handler
	licenseHandler := &api.LicenseHandler{
		Manager: licenseManager,
	}

	// 4. Routes
	mux := http.NewServeMux()

	// Public Routes
	// Wrap Login with special limiter? Or just allow Global/Endpoint limit to handle it?
	// Phase 1.4 Step E: "Add specific tighter limits for... login".
	// The GlobalLimiter middleware handles Endpoint limits if we pass them.
	// We passed `rootCfg.RateLimit.Endpoints`.
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/v1/auth/logout", authHandler.Logout)
	// Phase 1.7: Public Complete Reset
	mux.HandleFunc("/api/v1/auth/complete-reset", userHandler.CompleteReset)

	// Protected Routes (Example)
	// We don't have business routes yet from other phases, but we simulate one to prove wiring.
	// "GET /api/v1/cameras" -> Require "cameras.list" (site scope)
	// This acts as the "Verification" that middleware works in the binary.

	// Global JWT Guard for /api/v1/protected/
	// We also verify permission for the debug route to use permsMiddleware
	// Require "debug.view" (tenant scope)
	debugHandler := permsMiddleware.RequirePermission("debug.view", "tenant")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, _ := middleware.GetAuthContext(r.Context())
		fmt.Fprintf(w, "Hello Tenant:%s User:%s", ac.TenantID, ac.UserID)
	}))

	// Audit APIs (Req audit.read or audit.export)
	// Query: audit.read
	auditQueryHandler := permsMiddleware.RequirePermission("audit.read", "tenant")(http.HandlerFunc(auditHandler.GetEvents))
	// Export: audit.export
	auditExportHandler := permsMiddleware.RequirePermission("audit.export", "tenant")(http.HandlerFunc(auditHandler.ExportEvents))

	// License APIs
	// GET Status: license.read
	licenseStatusHandler := permsMiddleware.RequirePermission("license.read", "tenant")(http.HandlerFunc(licenseHandler.GetStatus))
	// POST Reload: license.manage
	licenseReloadHandler := permsMiddleware.RequirePermission("license.manage", "tenant")(http.HandlerFunc(licenseHandler.Reload))

	// User Management Routes (Phase 1.7)
	// Base User CRUD
	// GET /api/v1/users/{id} -> user.read
	getUserHandler := permsMiddleware.RequirePermission("user.read", "tenant")(http.HandlerFunc(userHandler.GetUser))
	// POST /api/v1/users -> user.create
	createUserHandler := permsMiddleware.RequirePermission("user.create", "tenant")(http.HandlerFunc(userHandler.CreateUser))
	// POST :disable -> user.disable
	disableUserHandler := permsMiddleware.RequirePermission("user.disable", "tenant")(http.HandlerFunc(userHandler.DisableUser))
	// POST :reset-password (Admin) -> user.password.reset
	resetPwdHandler := permsMiddleware.RequirePermission("user.password.reset", "tenant")(http.HandlerFunc(userHandler.ResetPassword))
	// PUT :roles -> user.role.assign
	assignRoleHandler := permsMiddleware.RequirePermission("user.role.assign", "tenant")(http.HandlerFunc(userHandler.AssignRole))

	protectedMux := http.NewServeMux()
	protectedMux.Handle("/api/v1/debug/me", debugHandler)
	protectedMux.Handle("/api/v1/audit/events", auditQueryHandler)
	protectedMux.Handle("/api/v1/audit/exports", auditExportHandler)
	protectedMux.Handle("/api/v1/license/status", licenseStatusHandler)
	protectedMux.Handle("/api/v1/license/reload", licenseReloadHandler)

	// Phase 1.7 Routes
	protectedMux.Handle("/api/v1/users/{id}", getUserHandler) // Note: http.ServeMux pattern matching for {id} requires Go 1.22
	protectedMux.Handle("POST /api/v1/users", createUserHandler)
	protectedMux.Handle("POST /api/v1/users/{id}/disable", disableUserHandler)
	protectedMux.Handle("POST /api/v1/users/{id}/reset-password", resetPwdHandler)
	protectedMux.Handle("PUT /api/v1/users/{id}/roles", assignRoleHandler)

	// Chain: JWT -> Permission (Optional per route) -> Handler
	// We mount protectedMux under a path protected by JWT

	// Global JWT Guard for /api/v1/protected/
	mux.Handle("/api/v1/protected/", http.StripPrefix("/api/v1/protected", jwtMiddleware.Middleware(protectedMux)))

	// Wrap TOP Level Mux with Global Rate Limiter -> Audit Logger
	// Order: RateLimit -> Audit (Log accepted requests) -> Mux
	// If RateLimit blocks, Audit middleware won't run (or should it? Usually no, unless we audit blocks too.
	// Prompt says "Log all mutating requests". If blocked 429, it never reached mux.
	// But `finalHandler` wraps mux.
	// Correct chain: RateLimit (outer) -> Audit (inner) -> Mux.
	// If RateLimit blocks, it returns early. Audit assumes request reached app logic.
	// If user wants to audit ratelimits, audit should be OUTER.
	// Prompt D: "Automatic HTTP Request Logging... Log all mutating...".
	// Usually audit successful or app-level failures.
	// Let's put Audit right before Mux (after Rate Limit).

	// RateLimit -> Audit -> Mux
	auditWrappedMux := auditMiddleware.LogRequest(mux)
	finalHandler := rlMiddleware.GlobalLimiter(auditWrappedMux)

	log.Println("Starting server on :8080")
	server := &http.Server{
		Addr:    ":8080",
		Handler: finalHandler,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			elog.Error(eventIDError, fmt.Sprintf("HTTP server error: %v", err))
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for stop signal (Service Stop or Interrupt)
	// For simplicity in this phase, we wait on stopChan if in service mode,
	// or block indefinitely (or handle signals) in console mode.
	if isService {
		<-stopChan
		elog.Info(eventIDStop, "Service stop requested")
	} else {
		// In console mode, we could handle Ctrl+C here if desired,
		// but standard log.Fatal on server start is fine for now.
		select {}
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		elog.Error(eventIDError, fmt.Sprintf("Graceful shutdown error: %v", err))
	}
	elog.Info(eventIDStop, "Server stopped gracefully")
}
