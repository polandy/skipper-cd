package icons

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPFetcher fetches icons as SVGs from an icon set served over HTTP
// (dashboard-icons by default). A 404 is a definitive miss; any other non-200
// status is a transient error.
type HTTPFetcher struct {
	// BaseURL is the icon set root; the request path is BaseURL/<slug>.svg.
	BaseURL string
	// Client sends the requests. When nil, http.DefaultClient is used; wire a
	// Client with a Timeout in production so a slow source cannot hang.
	Client *http.Client
}

// NewHTTPFetcher returns an HTTPFetcher for the given icon set base URL using
// client (nil selects http.DefaultClient).
func NewHTTPFetcher(baseURL string, client *http.Client) *HTTPFetcher {
	return &HTTPFetcher{BaseURL: strings.TrimRight(baseURL, "/"), Client: client}
}

// Fetch implements Fetcher.
func (f *HTTPFetcher) Fetch(ctx context.Context, slug string) (data []byte, contentType string, found bool, err error) {
	url := f.BaseURL + "/" + slug + ".svg"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", false, err
		}
		return data, "image/svg+xml", true, nil
	case http.StatusNotFound:
		return nil, "", false, nil
	default:
		return nil, "", false, fmt.Errorf("icon source returned %s for %q", resp.Status, slug)
	}
}

func (f *HTTPFetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return http.DefaultClient
}
