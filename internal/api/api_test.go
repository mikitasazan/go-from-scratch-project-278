package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"code/internal/api"
	"code/internal/store"
)

const baseURL = "https://short.test"

// fakeStore is an in-memory stand-in for the generated queries: enough to drive
// the handlers without a database, including the unique-name conflict.
type fakeStore struct {
	links  map[int64]store.Link
	nextID int64
	fail   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{links: map[int64]store.Link{}, nextID: 1}
}

func duplicateErr() error {
	return &pgconn.PgError{Code: "23505"}
}

func (f *fakeStore) ListLinks(_ context.Context) ([]store.Link, error) {
	if f.fail != nil {
		return nil, f.fail
	}

	out := make([]store.Link, 0, len(f.links))
	for id := int64(1); id < f.nextID; id++ {
		if link, ok := f.links[id]; ok {
			out = append(out, link)
		}
	}

	return out, nil
}

func (f *fakeStore) ListLinksRange(ctx context.Context, arg store.ListLinksRangeParams) ([]store.Link, error) {
	all, err := f.ListLinks(ctx)
	if err != nil {
		return nil, err
	}

	from := min(int(arg.Offset), len(all))
	to := min(from+int(arg.Limit), len(all))

	return all[from:to], nil
}

func (f *fakeStore) CountLinks(ctx context.Context) (int64, error) {
	all, err := f.ListLinks(ctx)
	if err != nil {
		return 0, err
	}

	return int64(len(all)), nil
}

func (f *fakeStore) GetLink(_ context.Context, id int64) (store.Link, error) {
	link, ok := f.links[id]
	if !ok {
		return store.Link{}, pgx.ErrNoRows
	}

	return link, nil
}

func (f *fakeStore) GetLinkByShortName(_ context.Context, name string) (store.Link, error) {
	for _, link := range f.links {
		if link.ShortName == name {
			return link, nil
		}
	}

	return store.Link{}, pgx.ErrNoRows
}

func (f *fakeStore) taken(name string, exceptID int64) bool {
	for id, link := range f.links {
		if link.ShortName == name && id != exceptID {
			return true
		}
	}

	return false
}

func (f *fakeStore) CreateLink(_ context.Context, arg store.CreateLinkParams) (store.Link, error) {
	if f.taken(arg.ShortName, 0) {
		return store.Link{}, duplicateErr()
	}

	link := store.Link{ID: f.nextID, OriginalUrl: arg.OriginalUrl, ShortName: arg.ShortName}
	f.links[link.ID] = link
	f.nextID++

	return link, nil
}

func (f *fakeStore) UpdateLink(_ context.Context, arg store.UpdateLinkParams) (store.Link, error) {
	if _, ok := f.links[arg.ID]; !ok {
		return store.Link{}, pgx.ErrNoRows
	}

	if f.taken(arg.ShortName, arg.ID) {
		return store.Link{}, duplicateErr()
	}

	link := store.Link{ID: arg.ID, OriginalUrl: arg.OriginalUrl, ShortName: arg.ShortName}
	f.links[arg.ID] = link

	return link, nil
}

func (f *fakeStore) DeleteLink(_ context.Context, id int64) (int64, error) {
	if _, ok := f.links[id]; !ok {
		return 0, nil
	}

	delete(f.links, id)

	return 1, nil
}

func newTestRouter(s api.LinkStore) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	api.NewHandler(s, baseURL).Register(router)

	return router
}

func do(t *testing.T, router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("cannot decode %s: %v", recorder.Body.String(), err)
	}
}

func TestCreateLinkWithGivenShortName(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/long-url","short_name":"exmpl"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusCreated, recorder.Body)
	}

	var got map[string]any
	decode(t, recorder, &got)

	if got["short_name"] != "exmpl" {
		t.Fatalf("short_name = %v, want exmpl", got["short_name"])
	}

	if got["short_url"] != baseURL+"/r/exmpl" {
		t.Fatalf("short_url = %v", got["short_url"])
	}
}

func TestCreateLinkGeneratesShortName(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/long-url"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusCreated, recorder.Body)
	}

	var got map[string]any
	decode(t, recorder, &got)

	name, _ := got["short_name"].(string)
	if len(name) != 6 {
		t.Fatalf("generated short_name = %q, want 6 characters", name)
	}
}

func TestCreateLinkRejectsInvalidBody(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodPost, "/api/links", `{"original_url":"not-a-url"}`)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateLinkRejectsTakenShortName(t *testing.T) {
	router := newTestRouter(newFakeStore())

	body := `{"original_url":"https://example.com/long-url","short_name":"exmpl"}`
	do(t, router, http.MethodPost, "/api/links", body)

	recorder := do(t, router, http.MethodPost, "/api/links", body)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestListLinks(t *testing.T) {
	router := newTestRouter(newFakeStore())

	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/one","short_name":"one"}`)
	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/two","short_name":"two"}`)

	recorder := do(t, router, http.MethodGet, "/api/links", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var got []map[string]any
	decode(t, recorder, &got)

	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}
}

func TestListLinksIsEmptyArray(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodGet, "/api/links", "")

	if body := recorder.Body.String(); body != "[]" {
		t.Fatalf("body = %s, want []", body)
	}
}

func TestGetLink(t *testing.T) {
	router := newTestRouter(newFakeStore())

	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/one","short_name":"one"}`)

	recorder := do(t, router, http.MethodGet, "/api/links/1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var got map[string]any
	decode(t, recorder, &got)

	if got["original_url"] != "https://example.com/one" {
		t.Fatalf("original_url = %v", got["original_url"])
	}
}

func TestGetMissingLinkIsNotFound(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodGet, "/api/links/404", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGetLinkWithNonNumericIDIsNotFound(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodGet, "/api/links/abc", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestUpdateLink(t *testing.T) {
	router := newTestRouter(newFakeStore())

	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/one","short_name":"one"}`)

	recorder := do(t, router, http.MethodPut, "/api/links/1",
		`{"original_url":"https://example.com/changed","short_name":"changed"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body)
	}

	var got map[string]any
	decode(t, recorder, &got)

	if got["original_url"] != "https://example.com/changed" || got["short_name"] != "changed" {
		t.Fatalf("link not updated: %v", got)
	}
}

func TestUpdateMissingLinkIsNotFound(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodPut, "/api/links/404",
		`{"original_url":"https://example.com/changed","short_name":"changed"}`)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestUpdateRejectsTakenShortName(t *testing.T) {
	router := newTestRouter(newFakeStore())

	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/one","short_name":"one"}`)
	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/two","short_name":"two"}`)

	recorder := do(t, router, http.MethodPut, "/api/links/2",
		`{"original_url":"https://example.com/two","short_name":"one"}`)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestDeleteLink(t *testing.T) {
	router := newTestRouter(newFakeStore())

	do(t, router, http.MethodPost, "/api/links",
		`{"original_url":"https://example.com/one","short_name":"one"}`)

	recorder := do(t, router, http.MethodDelete, "/api/links/1", "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	if body := recorder.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}

	if again := do(t, router, http.MethodGet, "/api/links/1", ""); again.Code != http.StatusNotFound {
		t.Fatalf("deleted link still readable: %d", again.Code)
	}
}

func TestDeleteMissingLinkIsNotFound(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodDelete, "/api/links/404", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func seed(t *testing.T, router *gin.Engine, count int) {
	t.Helper()

	for i := 1; i <= count; i++ {
		body := fmt.Sprintf(`{"original_url":"https://example.com/%d","short_name":"name%d"}`, i, i)
		if recorder := do(t, router, http.MethodPost, "/api/links", body); recorder.Code != http.StatusCreated {
			t.Fatalf("seeding failed at %d: %d", i, recorder.Code)
		}
	}
}

func TestListLinksFirstPage(t *testing.T) {
	router := newTestRouter(newFakeStore())
	seed(t, router, 12)

	recorder := do(t, router, http.MethodGet, "/api/links?range=[0,10]", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var got []map[string]any
	decode(t, recorder, &got)

	if len(got) != 10 {
		t.Fatalf("got %d links, want 10", len(got))
	}

	if got[0]["short_name"] != "name1" || got[9]["short_name"] != "name10" {
		t.Fatalf("wrong page: %v .. %v", got[0]["short_name"], got[9]["short_name"])
	}

	if header := recorder.Header().Get("Content-Range"); header != "links 0-10/12" {
		t.Fatalf("Content-Range = %q, want %q", header, "links 0-10/12")
	}
}

func TestListLinksSecondPage(t *testing.T) {
	router := newTestRouter(newFakeStore())
	seed(t, router, 11)

	recorder := do(t, router, http.MethodGet, "/api/links?range=%5B5,%2010%5D", "")

	var got []map[string]any
	decode(t, recorder, &got)

	if len(got) != 5 {
		t.Fatalf("got %d links, want 5", len(got))
	}

	if got[0]["short_name"] != "name6" || got[4]["short_name"] != "name10" {
		t.Fatalf("wrong page: %v .. %v", got[0]["short_name"], got[4]["short_name"])
	}

	if header := recorder.Header().Get("Content-Range"); header != "links 5-10/11" {
		t.Fatalf("Content-Range = %q, want %q", header, "links 5-10/11")
	}
}

func TestListLinksRangePastTheEnd(t *testing.T) {
	router := newTestRouter(newFakeStore())
	seed(t, router, 3)

	recorder := do(t, router, http.MethodGet, "/api/links?range=[10,20]", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	if body := recorder.Body.String(); body != "[]" {
		t.Fatalf("body = %s, want []", body)
	}
}

func TestListLinksWithoutRangeReportsWholeCollection(t *testing.T) {
	router := newTestRouter(newFakeStore())
	seed(t, router, 3)

	recorder := do(t, router, http.MethodGet, "/api/links", "")

	if header := recorder.Header().Get("Content-Range"); header != "links 0-3/3" {
		t.Fatalf("Content-Range = %q, want %q", header, "links 0-3/3")
	}
}

func TestListLinksRejectsBrokenRange(t *testing.T) {
	router := newTestRouter(newFakeStore())

	recorder := do(t, router, http.MethodGet, "/api/links?range=broken", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}
