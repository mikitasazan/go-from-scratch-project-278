package main

import (
	"log"
	"net/http"
	"os"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"

	sentry "github.com/getsentry/sentry-go"
)

const (
	defaultPort      = "8080"
	sentryFlushLimit = 2 * time.Second
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

func newRouter(withSentry bool) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	if withSentry {
		router.Use(sentrygin.New(sentrygin.Options{Repanic: true}))
	}

	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	return router
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}

	return defaultPort
}

func main() {
	withSentry := initSentry()
	if withSentry {
		defer sentry.Flush(sentryFlushLimit)
	}

	if err := newRouter(withSentry).Run(":" + port()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
