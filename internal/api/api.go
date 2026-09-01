// Package api holds the HTTP layer of the link shortener: request handling,
// validation and the JSON shape the service answers with.
package api

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"code/internal/store"
)

const (
	shortNameLength   = 6
	shortNameAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// generateAttempts caps the retries when a generated short name collides
	// with an existing one. Collisions are rare, so a small cap is enough.
	generateAttempts = 5
	uniqueViolation  = "23505"
)

// ErrShortNameTaken is returned when the requested short name already exists.
var ErrShortNameTaken = errors.New("short name is already taken")

// LinkStore is the slice of the generated queries the HTTP layer needs. Keeping
// it an interface lets the handlers be tested without a database.
type LinkStore interface {
	ListLinks(ctx context.Context) ([]store.Link, error)
	GetLink(ctx context.Context, id int64) (store.Link, error)
	GetLinkByShortName(ctx context.Context, shortName string) (store.Link, error)
	CreateLink(ctx context.Context, arg store.CreateLinkParams) (store.Link, error)
	UpdateLink(ctx context.Context, arg store.UpdateLinkParams) (store.Link, error)
	DeleteLink(ctx context.Context, id int64) (int64, error)
}

// Handler serves the link API.
type Handler struct {
	store   LinkStore
	baseURL string
}

// NewHandler builds a handler. baseURL is the public address the short links
// are built from, so it changes per environment.
func NewHandler(s LinkStore, baseURL string) *Handler {
	return &Handler{store: s, baseURL: strings.TrimRight(baseURL, "/")}
}

// linkResponse is the JSON shape of a link, short_url included.
type linkResponse struct {
	ID          int64  `json:"id"`
	OriginalURL string `json:"original_url"`
	ShortName   string `json:"short_name"`
	ShortURL    string `json:"short_url"`
}

type linkRequest struct {
	OriginalURL string `json:"original_url" binding:"required,url"`
	ShortName   string `json:"short_name"`
}

// Register wires the API routes onto the router.
func (h *Handler) Register(router gin.IRouter) {
	links := router.Group("/api/links")
	links.GET("", h.list)
	links.POST("", h.create)
	links.GET("/:id", h.get)
	links.PUT("/:id", h.update)
	links.DELETE("/:id", h.delete)
}

func (h *Handler) response(link store.Link) linkResponse {
	return linkResponse{
		ID:          link.ID,
		OriginalURL: link.OriginalUrl,
		ShortName:   link.ShortName,
		ShortURL:    h.baseURL + "/r/" + link.ShortName,
	}
}

func (h *Handler) list(c *gin.Context) {
	links, err := h.store.ListLinks(c.Request.Context())
	if err != nil {
		abortInternal(c, err)
		return
	}

	out := make([]linkResponse, 0, len(links))
	for _, link := range links {
		out = append(out, h.response(link))
	}

	c.JSON(http.StatusOK, out)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	link, err := h.store.GetLink(c.Request.Context(), id)
	if err != nil {
		abortStoreError(c, err)
		return
	}

	c.JSON(http.StatusOK, h.response(link))
}

func (h *Handler) create(c *gin.Context) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	link, err := h.createLink(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, ErrShortNameTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		abortInternal(c, err)

		return
	}

	c.JSON(http.StatusCreated, h.response(link))
}

// createLink inserts a link, generating a short name when none was given and
// retrying a generated one that happens to collide.
func (h *Handler) createLink(ctx context.Context, req linkRequest) (store.Link, error) {
	if req.ShortName != "" {
		link, err := h.store.CreateLink(ctx, store.CreateLinkParams{
			OriginalUrl: req.OriginalURL,
			ShortName:   req.ShortName,
		})
		if isUniqueViolation(err) {
			return store.Link{}, ErrShortNameTaken
		}

		return link, err
	}

	var lastErr error

	for range generateAttempts {
		name, err := generateShortName()
		if err != nil {
			return store.Link{}, err
		}

		link, err := h.store.CreateLink(ctx, store.CreateLinkParams{
			OriginalUrl: req.OriginalURL,
			ShortName:   name,
		})
		if err == nil {
			return link, nil
		}

		if !isUniqueViolation(err) {
			return store.Link{}, err
		}

		lastErr = err
	}

	return store.Link{}, lastErr
}

func (h *Handler) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	shortName := req.ShortName
	if shortName == "" {
		current, err := h.store.GetLink(c.Request.Context(), id)
		if err != nil {
			abortStoreError(c, err)
			return
		}

		shortName = current.ShortName
	}

	link, err := h.store.UpdateLink(c.Request.Context(), store.UpdateLinkParams{
		ID:          id,
		OriginalUrl: req.OriginalURL,
		ShortName:   shortName,
	})

	if isUniqueViolation(err) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrShortNameTaken.Error()})
		return
	}

	if err != nil {
		abortStoreError(c, err)
		return
	}

	c.JSON(http.StatusOK, h.response(link))
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	affected, err := h.store.DeleteLink(c.Request.Context(), id)
	if err != nil {
		abortInternal(c, err)
		return
	}

	if affected == 0 {
		abortNotFound(c)
		return
	}

	c.Status(http.StatusNoContent)
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortNotFound(c)
		return 0, false
	}

	return id, true
}

func abortNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
}

func abortInternal(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}

func abortStoreError(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		abortNotFound(c)
		return
	}

	abortInternal(c, err)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func generateShortName() (string, error) {
	buf := make([]byte, shortNameLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	for i, b := range buf {
		buf[i] = shortNameAlphabet[int(b)%len(shortNameAlphabet)]
	}

	return string(buf), nil
}
