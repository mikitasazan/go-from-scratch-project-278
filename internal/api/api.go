// Package api holds the HTTP layer of the link shortener: request handling,
// validation and the JSON shape the service answers with.
package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
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
	// redirectStatus is the code /r/:code answers with, and the one written
	// into the visit record.
	redirectStatus = http.StatusFound
)

// ErrShortNameTaken is returned when the requested short name already exists.
var ErrShortNameTaken = errors.New("short name is already taken")

// shortNamePattern is the set a short name may use: it becomes a single path
// segment, so a slash or a space would break the link it names.
var shortNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// LinkStore is the slice of the generated queries the HTTP layer needs. Keeping
// it an interface lets the handlers be tested without a database.
type LinkStore interface {
	ListLinks(ctx context.Context) ([]store.Link, error)
	ListLinksRange(ctx context.Context, arg store.ListLinksRangeParams) ([]store.Link, error)
	CountLinks(ctx context.Context) (int64, error)
	GetLink(ctx context.Context, id int64) (store.Link, error)
	GetLinkByShortName(ctx context.Context, shortName string) (store.Link, error)
	CreateLink(ctx context.Context, arg store.CreateLinkParams) (store.Link, error)
	UpdateLink(ctx context.Context, arg store.UpdateLinkParams) (store.Link, error)
	DeleteLink(ctx context.Context, id int64) (int64, error)
	CreateLinkVisit(ctx context.Context, arg store.CreateLinkVisitParams) (store.LinkVisit, error)
	ListLinkVisits(ctx context.Context) ([]store.LinkVisit, error)
	ListLinkVisitsRange(ctx context.Context, arg store.ListLinkVisitsRangeParams) ([]store.LinkVisit, error)
	CountLinkVisits(ctx context.Context) (int64, error)
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

// visitResponse is the JSON shape of one recorded visit.
type visitResponse struct {
	ID        int64  `json:"id"`
	LinkID    int64  `json:"link_id"`
	CreatedAt string `json:"created_at"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Referer   string `json:"referer"`
	// Reffer repeats Referer under the name the supplied frontend actually
	// reads (its own column is spelled "reffer"), so the interface shows the
	// value while the field the step text names stays in the response.
	Reffer string `json:"reffer"`
	Status int32  `json:"status"`
}

// createLinkPayload is the body of a create or update request. The type name
// shows up in the validator's own messages, which the step's example quotes.
type createLinkPayload struct {
	OriginalURL string `json:"original_url" binding:"required,url"`
	ShortName   string `json:"short_name" binding:"omitempty,min=3,max=32,shortname"`
}

// init makes the validator name a field by its JSON tag, so an error is keyed
// by `original_url` rather than by the Go field name.
func init() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}

	// A short name goes straight into a URL path, so anything outside this set
	// would produce a short_url that /r/:code cannot match back.
	_ = v.RegisterValidation("shortname", func(fl validator.FieldLevel) bool {
		return shortNamePattern.MatchString(fl.Field().String())
	})

	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			return ""
		}

		return name
	})
}

// bindPayload reads and validates a request body, answering itself with the
// API's single error shape: 400 for a body that is not valid JSON at all, 422
// for one whose fields do not pass validation.
func bindPayload(c *gin.Context, payload *createLinkPayload) bool {
	err := c.ShouldBindJSON(payload)
	if err == nil {
		return true
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		fields := make(map[string]string, len(validationErrs))
		for _, fieldErr := range validationErrs {
			fields[fieldErr.Field()] = fieldErr.Error()
		}

		c.JSON(http.StatusUnprocessableEntity, gin.H{"errors": fields})

		return false
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})

	return false
}

// abortShortNameTaken answers a uniqueness conflict in the same shape as a
// validation failure, since from the caller's side it is one.
func abortShortNameTaken(c *gin.Context) {
	c.JSON(http.StatusUnprocessableEntity, gin.H{
		"errors": gin.H{"short_name": "short name already in use"},
	})
}

// Register wires the API routes onto the router.
func (h *Handler) Register(router gin.IRouter) {
	links := router.Group("/api/links")
	links.GET("", h.list)
	links.POST("", h.create)
	links.GET("/:id", h.get)
	links.PUT("/:id", h.update)
	links.DELETE("/:id", h.delete)

	router.GET("/api/link_visits", h.listVisits)
	router.GET("/r/:code", h.redirect)
}

func (h *Handler) response(link store.Link) linkResponse {
	return linkResponse{
		ID:          link.ID,
		OriginalURL: link.OriginalUrl,
		ShortName:   link.ShortName,
		ShortURL:    h.baseURL + "/r/" + link.ShortName,
	}
}

// parseRange reads the ?range=[start,end] parameter used for pagination. Both
// bounds are inclusive item positions, so [0,9] is the first ten links — the
// convention the step's own hint states and the one the supplied frontend
// sends (it asks for [0,4] and draws five rows). A missing parameter means
// "the whole collection".
func parseRange(raw string) (start, end int64, ok bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")

	parts := strings.Split(trimmed, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}

	end, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || end < start {
		return 0, 0, false
	}

	return start, end, true
}

// window is the slice of a collection one list request asks for.
type window struct {
	start, end int64
	limited    bool
}

// bounds turns the requested window into a limit and an offset that are safe to
// hand to the database. A range far past the end of the collection asks for
// nothing rather than wrapping around: the driver takes int32, and an unclamped
// int64 silently becomes a negative limit.
func (w window) bounds(total int64) (limit, offset int32) {
	if w.start >= total {
		return 0, 0
	}

	last := min(w.end, total-1)
	if last < w.start {
		return 0, 0
	}

	return int32(last - w.start + 1), int32(w.start)
}

// resolveWindow reads ?range and turns it into a window over a collection of
// the given size. It answers 422 itself when the parameter is malformed.
func resolveWindow(c *gin.Context, total int64) (window, bool) {
	raw, hasRange := c.GetQuery("range")
	if !hasRange {
		return window{start: 0, end: max(total-1, 0)}, true
	}

	start, end, ok := parseRange(raw)
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"errors": gin.H{"range": "range must look like [0,9]"},
		})

		return window{}, false
	}

	return window{start: start, end: end, limited: true}, true
}

// listCollection is the shape every list endpoint here has: count the rows,
// work out the window, fetch it, and say in the header what was served.
func listCollection[T any, R any](
	c *gin.Context,
	name string,
	count func(context.Context) (int64, error),
	all func(context.Context) ([]T, error),
	page func(context.Context, int32, int32) ([]T, error),
	convert func(T) R,
) {
	ctx := c.Request.Context()

	total, err := count(ctx)
	if err != nil {
		abortInternal(c, err)
		return
	}

	win, ok := resolveWindow(c, total)
	if !ok {
		return
	}

	var rows []T

	if win.limited {
		limit, offset := win.bounds(total)
		if limit == 0 {
			rows = nil
		} else {
			rows, err = page(ctx, limit, offset)
		}
	} else {
		rows, err = all(ctx)
	}

	if err != nil {
		abortInternal(c, err)
		return
	}

	out := make([]R, 0, len(rows))
	for _, row := range rows {
		out = append(out, convert(row))
	}

	c.Header("Content-Range", fmt.Sprintf("%s %d-%d/%d", name, win.start, win.end, total))
	c.JSON(http.StatusOK, out)
}

func (h *Handler) list(c *gin.Context) {
	listCollection(c, "links",
		h.store.CountLinks,
		h.store.ListLinks,
		func(ctx context.Context, limit, offset int32) ([]store.Link, error) {
			return h.store.ListLinksRange(ctx, store.ListLinksRangeParams{Limit: limit, Offset: offset})
		},
		h.response,
	)
}

func (h *Handler) listVisits(c *gin.Context) {
	listCollection(c, "link_visits",
		h.store.CountLinkVisits,
		h.store.ListLinkVisits,
		func(ctx context.Context, limit, offset int32) ([]store.LinkVisit, error) {
			return h.store.ListLinkVisitsRange(ctx, store.ListLinkVisitsRangeParams{Limit: limit, Offset: offset})
		},
		visitFrom,
	)
}

func visitFrom(visit store.LinkVisit) visitResponse {
	return visitResponse{
		ID:        visit.ID,
		LinkID:    visit.LinkID,
		CreatedAt: visit.CreatedAt.Time.UTC().Format(time.RFC3339),
		IP:        visit.Ip,
		UserAgent: visit.UserAgent,
		Referer:   visit.Referer,
		Reffer:    visit.Referer,
		Status:    visit.Status,
	}
}

// redirect sends the visitor to the original address and records the visit.
// A visit that cannot be written must not break the redirect itself.
func (h *Handler) redirect(c *gin.Context) {
	ctx := c.Request.Context()

	link, err := h.store.GetLinkByShortName(ctx, c.Param("code"))
	if err != nil {
		abortStoreError(c, err)
		return
	}

	_, err = h.store.CreateLinkVisit(ctx, store.CreateLinkVisitParams{
		LinkID:    link.ID,
		Ip:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Referer:   c.Request.Referer(),
		Status:    redirectStatus,
	})
	if err != nil {
		_ = c.Error(err)
	}

	c.Redirect(redirectStatus, link.OriginalUrl)
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
	var req createLinkPayload
	if !bindPayload(c, &req) {
		return
	}

	link, err := h.createLink(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, ErrShortNameTaken) {
			abortShortNameTaken(c)
			return
		}

		abortInternal(c, err)

		return
	}

	c.JSON(http.StatusCreated, h.response(link))
}

// createLink inserts a link, generating a short name when none was given and
// retrying a generated one that happens to collide.
func (h *Handler) createLink(ctx context.Context, req createLinkPayload) (store.Link, error) {
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

	if lastErr != nil {
		return store.Link{}, ErrShortNameTaken
	}

	return store.Link{}, nil
}

func (h *Handler) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req createLinkPayload
	if !bindPayload(c, &req) {
		return
	}

	link, err := h.store.UpdateLink(c.Request.Context(), store.UpdateLinkParams{
		ID:          id,
		OriginalUrl: req.OriginalURL,
		ShortName:   req.ShortName,
	})

	if isUniqueViolation(err) {
		abortShortNameTaken(c)
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

// generateShortName draws letters uniformly. Taking a byte modulo 62 would
// make the first eight letters of the alphabet about a quarter more likely, so
// bytes that fall outside the last whole multiple of 62 are drawn again.
func generateShortName() (string, error) {
	const limit = 256 - 256%len(shortNameAlphabet)

	name := make([]byte, 0, shortNameLength)
	buf := make([]byte, shortNameLength)

	for len(name) < shortNameLength {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}

		for _, b := range buf {
			if int(b) >= limit {
				continue
			}

			name = append(name, shortNameAlphabet[int(b)%len(shortNameAlphabet)])
			if len(name) == shortNameLength {
				break
			}
		}
	}

	return string(name), nil
}
