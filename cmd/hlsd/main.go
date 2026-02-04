package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/hlsd"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/platform/paths"
	"github.com/technosupport/ts-vms/internal/platform/windows"
	"github.com/technosupport/ts-vms/internal/ratelimit"
	"github.com/technosupport/ts-vms/internal/tokens"
)

const serviceName = "TS-VMS-HLSD"

func main() {
	// 1. Windows Service Integration
	isService := windows.IsWindowsService()
	elog := windows.NewEventLogger(serviceName)
	defer elog.Close()

	stopChan := make(chan struct{})
	if isService {
		go func() {
			if err := windows.RunAsService(serviceName, stopChan); err != nil {
				elog.Error(102, fmt.Sprintf("Service run error: %v", err))
				os.Exit(1)
			}
		}()
	}

	// 2. Load Configuration & Secrets
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	redisAddr := os.Getenv("REDIS_ADDR")
	jwtKey := os.Getenv("JWT_SIGNING_KEY")
	hlsRoot := os.Getenv("HLS_ROOT_DIR")
	allowedOrigs := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")

	// HMAC Keys (kid rotation support)
	hmacKeys := make(map[string][]byte)
	for i := 1; i <= 5; i++ {
		k := os.Getenv(fmt.Sprintf("HLS_HMAC_KEY_V%d", i))
		if k != "" {
			hmacKeys[fmt.Sprintf("v%d", i)] = []byte(k)
		}
	}
	if len(hmacKeys) == 0 {
		hmacKeys["v1"] = []byte("dev-hls-secret") // Fallback for dev
	}

	if jwtKey == "" {
		jwtKey = "dev-secret-do-not-use-in-prod"
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	if hlsRoot == "" {
		hlsRoot = paths.ResolveDataRoot() + `\hls`
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPass, dbHost, dbName)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("DB open error: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	// 3. Components
	tokenMgr := tokens.NewManager(jwtKey)
	blacklist := auth.NewRedisBlacklist(rdb)
	camRepo := data.CameraModel{DB: db}
	permModel := data.PermissionModel{DB: db}
	permsMiddleware := middleware.NewPermissionMiddleware(permModel, camRepo)

	limiter := ratelimit.NewLimiter(rdb, "hlsd-salt")
	rlCfg := middleware.Config{
		GlobalIP: ratelimit.LimitConfig{Rate: 100, Window: time.Second},
		User:     ratelimit.LimitConfig{Rate: 1000, Window: time.Hour},
	}
	rlMiddleware := middleware.NewRateLimitMiddleware(limiter, tokenMgr, rlCfg, nil)

	hlsHandler := hlsd.NewHandler(hlsd.Config{
		HlsRoot:        hlsRoot,
		AllowedOrigins: allowedOrigs,
		Keys:           &hlsd.MapKeyProvider{Keys: hmacKeys},
	}, permsMiddleware)

	// 4. Routing
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// CORS middleware - MUST be before JWT auth to handle preflight OPTIONS
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, Cookie")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Use(rlMiddleware.GlobalLimiter)

	// Health & Metrics
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	r.Handle("/metrics", promhttp.Handler())

	// Public session lookup (no auth required - just returns session ID)
	r.HandleFunc("/hls/session/{camera_id}", hlsHandler.GetActiveSession)

	// Protected HLS Routes (API uses JWT, HLS uses Token/Signature)
	jwtAuth := middleware.NewJWTAuth(tokenMgr, blacklist)

	// HLS Delivery w/ Custom Auth logic (HMAC Token + RBAC) - Must be outside standard JWT middleware
	hlsHandler.Register(r)

	r.Group(func(r chi.Router) {
		r.Use(jwtAuth.Middleware)
		// API endpoints can go here
	})

	// 5. Start Server
	port := os.Getenv("HLSD_PORT")
	if port == "" {
		port = "8081"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Printf("vms-hlsd listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 6. Graceful Shutdown
	if isService {
		<-stopChan
	} else {
		select {}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
