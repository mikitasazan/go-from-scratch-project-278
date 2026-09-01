package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	sentry "github.com/getsentry/sentry-go"

	"code/internal/api"
	"code/internal/store"
)

const (
	defaultPort      = "8080"
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

func newRouter(withSentry bool, handler *api.Handler) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

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

	if err := newRouter(withSentry, handler).Run(":" + port); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
