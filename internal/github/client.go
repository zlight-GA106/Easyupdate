package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type Asset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
}
type Release struct {
	ID          int64   `json:"id"`
	Tag         string  `json:"tag_name"`
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	Draft       bool    `json:"draft"`
	Assets      []Asset `json:"assets"`
}
type Client struct {
	token string
	http  *http.Client
}

func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many GitHub redirects")
		}
		if !allowedURL(req.URL) {
			return errors.New("unexpected GitHub redirect")
		}
		if req.URL.Hostname() != "api.github.com" {
			req.Header.Del("Authorization")
		}
		return nil
	}}}
}
func allowedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}
func (c *Client) api(ctx context.Context, path string, value any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "EasyUpdate/0.1")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("GitHub request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("GitHub HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil {
		return errors.New("GitHub response failed")
	}
	if len(data) > 4<<20 {
		return errors.New("GitHub response too large")
	}
	if err = json.Unmarshal(data, value); err != nil {
		return errors.New("invalid GitHub response")
	}
	return nil
}
func (c *Client) Releases(ctx context.Context, owner, repo string) ([]Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	var info struct {
		Private bool `json:"private"`
	}
	if err := c.api(ctx, prefix, &info); err != nil {
		return nil, err
	}
	if info.Private {
		return nil, errors.New("only public repositories are supported")
	}
	var items []Release
	if err := c.api(ctx, prefix+"/releases?per_page=100", &items); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].PublishedAt > items[j].PublishedAt })
	return items, nil
}
func (c *Client) Download(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !allowedURL(u) {
		return nil, errors.New("invalid GitHub asset URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "EasyUpdate/0.1")
	// The token is only sent to the API; public assets need no credentials.
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("GitHub asset download failed")
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("GitHub asset HTTP %d", res.StatusCode)
	}
	return res.Body, nil
}
