// Package github talks to GitHub Actions (environment files, workflow
// commands) and the GitHub REST API (pull request comments).
package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Context describes the workflow run, read from the Actions environment.
type Context struct {
	Token      string
	Repository string // owner/name
	APIURL     string
	ServerURL  string
	RunID      string
	EventName  string
	SHA        string
	PR         int
	HeadSHA    string
	Labels     []string
}

func FromEnv() (*Context, error) {
	c := &Context{
		Token:      firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN")),
		Repository: os.Getenv("GITHUB_REPOSITORY"),
		APIURL:     firstNonEmpty(os.Getenv("GITHUB_API_URL"), "https://api.github.com"),
		ServerURL:  firstNonEmpty(os.Getenv("GITHUB_SERVER_URL"), "https://github.com"),
		RunID:      os.Getenv("GITHUB_RUN_ID"),
		EventName:  os.Getenv("GITHUB_EVENT_NAME"),
		SHA:        os.Getenv("GITHUB_SHA"),
	}
	if path := os.Getenv("GITHUB_EVENT_PATH"); path != "" {
		if err := c.readEvent(path); err != nil {
			return nil, fmt.Errorf("reading event payload: %w", err)
		}
	}
	return c, nil
}

func (c *Context) readEvent(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	type labels []struct {
		Name string `json:"name"`
	}
	var ev struct {
		PullRequest *struct {
			Number int `json:"number"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
			Labels labels `json:"labels"`
		} `json:"pull_request"`
		Issue *struct {
			Number      int             `json:"number"`
			PullRequest json.RawMessage `json:"pull_request"`
			Labels      labels          `json:"labels"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	var ls labels
	switch {
	case ev.PullRequest != nil:
		c.PR = ev.PullRequest.Number
		c.HeadSHA = ev.PullRequest.Head.SHA
		ls = ev.PullRequest.Labels
	case ev.Issue != nil && len(ev.Issue.PullRequest) > 0:
		c.PR = ev.Issue.Number
		ls = ev.Issue.Labels
	}
	for _, l := range ls {
		c.Labels = append(c.Labels, l.Name)
	}
	return nil
}

// Commit is the commit the plan was made from: the PR head when known
// (GITHUB_SHA is a temporary merge commit on pull_request events).
func (c *Context) Commit() string { return firstNonEmpty(c.HeadSHA, c.SHA) }

// BlobURL is the base URL for links to files at Commit.
func (c *Context) BlobURL() string {
	if c.Repository == "" || c.Commit() == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/blob/%s", c.ServerURL, c.Repository, c.Commit())
}

// CommitURL links to Commit.
func (c *Context) CommitURL() string {
	if c.Repository == "" || c.Commit() == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/commit/%s", c.ServerURL, c.Repository, c.Commit())
}

// RunURL links to the current workflow run, whose page shows job summaries.
func (c *Context) RunURL() string {
	if c.Repository == "" || c.RunID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", c.ServerURL, c.Repository, c.RunID)
}

type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// Client is a minimal REST client for issue comments.
type Client struct {
	ctx  *Context
	http *http.Client
}

func NewClient(ctx *Context) (*Client, error) {
	if ctx.Token == "" {
		return nil, errors.New("GITHUB_TOKEN is not set")
	}
	if ctx.Repository == "" {
		return nil, errors.New("GITHUB_REPOSITORY is not set")
	}
	return &Client{ctx: ctx, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// FindComment returns the first comment on the PR containing marker.
func (c *Client) FindComment(pr int, marker string) (*Comment, error) {
	for page := 1; ; page++ {
		var comments []Comment
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", c.ctx.Repository, pr, page)
		if err := c.do("GET", path, nil, &comments); err != nil {
			return nil, err
		}
		for _, cm := range comments {
			if strings.Contains(cm.Body, marker) {
				return &cm, nil
			}
		}
		if len(comments) < 100 {
			return nil, nil
		}
	}
}

func (c *Client) CreateComment(pr int, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", c.ctx.Repository, pr)
	return c.do("POST", path, map[string]string{"body": body}, nil)
}

func (c *Client) UpdateComment(id int64, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/comments/%d", c.ctx.Repository, id)
	return c.do("PATCH", path, map[string]string{"body": body}, nil)
}

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(c.ctx.APIURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.ctx.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(msg))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// SetOutput writes a step output. No-op outside of Actions.
func SetOutput(name, value string) error {
	return appendEnvFile("GITHUB_OUTPUT", fmt.Sprintf("%s<<__TPR_EOF__\n%s\n__TPR_EOF__\n", name, value))
}

// AppendSummary appends Markdown to the job summary. No-op outside Actions.
func AppendSummary(markdown string) error {
	return appendEnvFile("GITHUB_STEP_SUMMARY", markdown+"\n")
}

func appendEnvFile(env, content string) error {
	path := os.Getenv(env)
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Annotation is a workflow command annotation shown on a file line in the
// pull request diff.
type Annotation struct {
	Level   string // error, warning or notice
	File    string
	Line    int
	Title   string
	Message string
}

// Command formats the annotation as a workflow command.
func (a Annotation) Command() string {
	return fmt.Sprintf("::%s file=%s,line=%d,title=%s::%s",
		a.Level, escapeProperty(a.File), a.Line, escapeProperty(a.Title), escapeData(a.Message))
}

func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
