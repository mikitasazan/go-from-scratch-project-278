package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	sentry "github.com/getsentry/sentry-go"

	"code/internal/api"
	"code/internal/store"
)

const (
	defaultPort      = "8080"
	defaultFrontend  = "http://localhost:5173"
	sentryFlushLimit = 2 * time.Second
	connectTimeout   = 10 * time.Second
)

// initSentry wires error monitoring when SENTRY_DSN is set. An empty DSN keeps
// the service running without monitoring, which is the local-development case.
func initSentry() bool {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return false
	}

	if err := sentry.Init(sentry.ClientOptions{Dsn: dsn}); err != nil {
		log.Printf("sentry disabled: %v", err)
		return false
	}

	return true
}

func newRouter(withSentry bool, handler *api.Handler, allowedOrigins []string) *gin.Engine {
	router := gin.New()

	// Behind Cloudflare (which is what Render puts in front of a service) the
	// client address arrives in CF-Connecting-IP. Gin trusts that header without
	// checking who sent it, so switching it on anywhere else would let a caller
	// write any address it likes into the visit log — hence the env switch,
	// off by default for a service that is not behind Cloudflare.
	if os.Getenv("TRUST_CLOUDFLARE") == "true" {
		router.TrustedPlatform = gin.PlatformCloudflare
	}

	router.Use(gin.Logger(), gin.Recovery())

	if len(allowedOrigins) > 0 {
		router.Use(cors.New(cors.Config{
			AllowOrigins:  allowedOrigins,
			AllowMethods:  []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
			AllowHeaders:  []string{"Origin", "Content-Type", "Accept"},
			ExposeHeaders: []string{"Content-Range"},
			MaxAge:        12 * time.Hour,
		}))
	}

	if withSentry {
		router.Use(sentrygin.New(sentrygin.Options{Repanic: true}))
	}

	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	if handler != nil {
		handler.Register(router)
	}

	return router
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func main() {
	// A missing .env is normal outside local development.
	_ = godotenv.Load()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("cannot open the database: %v", err)
	}

	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("cannot reach the database: %v", err)
	}

	withSentry := initSentry()
	if withSentry {
		defer sentry.Flush(sentryFlushLimit)
	}

	port := env("PORT", defaultPort)
	handler := api.NewHandler(store.New(pool), env("BASE_URL", "http://localhost:"+port))

	origins := strings.Split(env("CORS_ORIGINS", defaultFrontend), ",")

	if err := newRouter(withSentry, handler, origins).Run(":" + port); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
