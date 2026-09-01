package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

const defaultPort = ":8080"

func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	return router
}

func main() {
	if err := newRouter().Run(defaultPort); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
