package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed static
var staticFiles embed.FS

var mux = http.NewServeMux()

type Config struct {
	Port        string
	DatabaseURL string
	RedisAddr   string
	SeedDemo    bool
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() Config {
	return Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/mosquito?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "localhost:6379"),
		SeedDemo:    env("SEED_DEMO", "true") == "true",
	}
}

func main() {
	cfg := loadConfig()
	initDB(cfg.DatabaseURL)
	initRedis(cfg.RedisAddr)
	migrate()
	if cfg.SeedDemo {
		seed()
	}
	// 历史物业积水整改任务按统一的“同一积水点”口径可追溯补齐投诉基线/当前数
	backfillPropRectComplaints()

	registerAuthRoutes()
	registerReportRoutes()
	registerDispatchRoutes()
	registerOrderRoutes()
	registerDashboardRoutes()
	registerChildZoneRoutes()
	registerPetRoutes()
	registerPropertyRectRoutes()
	registerEmergencyRoutes()
	registerAccessRoutes()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			jsonErr(w, 503, "database not ready")
			return
		}
		if err := rdb.Ping(r.Context()).Err(); err != nil {
			jsonErr(w, 503, "redis not ready")
			return
		}
		jsonOK(w, map[string]string{"status": "ok"})
	})

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("mosquito control service listening on :%s", cfg.Port)
	log.Fatal(srv.ListenAndServe())
}
