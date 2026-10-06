package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI serves the issue comment endpoints from memory.
type fakeAPI struct {
	comments []Comment
	nextID   int64
	requests []string
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		var in struct{ Body string }
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/issues/1/comments":
			page := 1
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			start, end := (page-1)*100, page*100
			start, end = min(start, len(f.comments)), min(end, len(f.comments))
			_ = json.NewEncoder(w).Encode(f.comments[start:end])
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/issues/1/comments":
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.nextID++
			f.comments = append(f.comments, Comment{ID: f.nextID, Body: in.Body})
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{}"))
		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
			_ = json.NewDecoder(r.Body).Decode(&in)
			var id int64
			fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), &id)
			for i := range f.comments {
				if f.comments[i].ID == id {
					f.comments[i].Body = in.Body
				}
			}
			_, _ = w.Write([]byte("{}"))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	})
}

func newTestClient(t *testing.T, f *fakeAPI) *Client {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	c, err := NewClient(&Context{Token: "test-token", Repository: "o/r", APIURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFindCommentPaginates(t *testing.T) {
	f := &fakeAPI{}
	for i := range 150 {
		f.comments = append(f.comments, Comment{ID: int64(i + 1), Body: "unrelated"})
	}
	f.comments[120].Body = "hello <!-- marker -->"
	c := newTestClient(t, f)

	got, err := c.FindComment(1, "<!-- marker -->")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != 121 {
		t.Fatalf("got %+v", got)
	}
	if len(f.requests) != 2 {
		t.Errorf("want 2 page requests, got %v", f.requests)
	}

	none, err := c.FindComment(1, "<!-- other -->")
	if err != nil || none != nil {
		t.Errorf("got %+v, %v", none, err)
	}
}

func TestCreateAndUpdate(t *testing.T) {
	f := &fakeAPI{}
	c := newTestClient(t, f)
	if err := c.CreateComment(1, "first"); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateComment(f.comments[0].ID, "second"); err != nil {
		t.Fatal(err)
	}
	if len(f.comments) != 1 || f.comments[0].Body != "second" {
		t.Errorf("comments = %+v", f.comments)
	}
}

func TestAPIErrorsIncludeResponse(t *testing.T) {
	f := &fakeAPI{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	c, _ := NewClient(&Context{Token: "wrong", Repository: "o/r", APIURL: srv.URL})
	err := c.CreateComment(1, "x")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Bad credentials") {
		t.Errorf("err = %v", err)
	}
}

func TestNewClientRequiresEnv(t *testing.T) {
	if _, err := NewClient(&Context{Repository: "o/r"}); err == nil {
		t.Error("missing token should fail")
	}
	if _, err := NewClient(&Context{Token: "t"}); err == nil {
		t.Error("missing repository should fail")
	}
}
