package ui

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipEncoding is the Content-Encoding / Accept-Encoding token for gzip, used
// by both ServeHTTP (to set the header) and acceptsGzip (to match it).
const gzipEncoding = "gzip"

// staticAsset serves a fixed byte payload with gzip content negotiation. The
// gzip representation is computed once at construction time — handlers that
// use it already build their payload once (e.g. IndexHandler bakes in the
// configured theme) — so a gzip-capable request never pays compression cost
// per request. Used for the app shell's larger embedded text assets
// (index.html, app.css, app.js, app-render.js, app-helpers.js); fonts and
// icons are already
// compressed binary formats and gain nothing from it.
type staticAsset struct {
	contentType string
	plain       []byte
	gzip        []byte
}

func newStaticAsset(contentType string, data []byte) staticAsset {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		panic(err) // bytes.Buffer.Write never returns an error
	}
	if err := gw.Close(); err != nil {
		panic(err) // flushing gzip.Writer into a bytes.Buffer never errors
	}
	return staticAsset{contentType: contentType, plain: data, gzip: buf.Bytes()}
}

// ServeHTTP writes the Content-Type and, when the request's Accept-Encoding
// allows it, the pre-gzipped body with Content-Encoding: gzip; otherwise the
// plain body. Always sets Vary: Accept-Encoding so caches don't serve the
// wrong representation to a client that doesn't support gzip. Callers that
// need their own Cache-Control must set it before calling this.
func (a staticAsset) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Vary", "Accept-Encoding")
	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", gzipEncoding)
		_, _ = w.Write(a.gzip)
		return
	}
	_, _ = w.Write(a.plain)
}

// acceptsGzip reports whether the request's Accept-Encoding header lists gzip
// as an acceptable encoding. It ignores q-values: this UI only ever offers
// gzip or identity, and no realistic client sends "gzip;q=0" to explicitly
// exclude the one encoding it would otherwise get for free.
func acceptsGzip(r *http.Request) bool {
	for enc := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(enc), ";")
		if strings.EqualFold(name, gzipEncoding) {
			return true
		}
	}
	return false
}
