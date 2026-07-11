package icons

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fetchFormats lists the icon formats tried in order. dashboard-icons stores
// each format in its own directory (svg/, png/, webp/) and not every icon
// exists as SVG, so png/webp are tried as fallbacks. SVG is preferred (small,
// scales crisp).
var fetchFormats = []struct {
	dir         string // icon-set subdirectory and file extension (without dot)
	contentType string
}{
	{"svg", "image/svg+xml"},
	{"png", "image/png"},
	{"webp", "image/webp"},
}

// HTTPFetcher fetches icons from an icon set served over HTTP (dashboard-icons
// by default), trying svg then png then webp. A slug absent in every format is
// a definitive miss; any non-200/404 status is a transient error.
type HTTPFetcher struct {
	// BaseURL is the icon set root; a request path is BaseURL/<fmt>/<slug>.<fmt>.
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

// Fetch implements Fetcher, trying each format in fetchFormats until one hits.
func (f *HTTPFetcher) Fetch(ctx context.Context, slug string) (data []byte, contentType string, found bool, err error) {
	for _, fmtSpec := range fetchFormats {
		data, found, err = f.fetchOne(ctx, fmtSpec.dir, slug)
		if err != nil {
			return nil, "", false, err
		}
		if found {
			return data, fmtSpec.contentType, true, nil
		}
	}
	return nil, "", false, nil
}

// fetchOne requests a single format. found is false only for a 404 (try the
// next format); any other non-200 status is returned as a transient error.
func (f *HTTPFetcher) fetchOne(ctx context.Context, format, slug string) (data []byte, found bool, err error) {
	url := f.BaseURL + "/" + format + "/" + slug + "." + format
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	case http.StatusNotFound:
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("icon source returned %s for %q", resp.Status, url)
	}
}

func (f *HTTPFetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return http.DefaultClient
}
