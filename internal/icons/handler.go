package icons

import "net/http"

// StackLocator maps a stack name to the inputs for resolving its icon. ok is
// false for an unknown stack name, which the handler answers with 404.
type StackLocator func(stackName string) (req Request, ok bool)

// Handler serves GET /api/icons/{stack}: it resolves the stack's icon and
// writes the image bytes. Any failure to resolve (unknown stack, no match, or
// a transient fetch error) is a 404, so the UI simply falls back to a monogram.
func Handler(svc *Service, locate StackLocator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := locate(r.PathValue("stack"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		res, err := svc.Resolve(r.Context(), req)
		if err != nil {
			// Both a definitive miss (ErrNotFound) and a transient fetch error
			// degrade to the monogram fallback; the latter is retried on the
			// next request because it is not cached.
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", res.ContentType)
		// Long cache; the refresh control busts it with a query parameter.
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(res.Data)
	})
}

// RefreshHandler serves POST /api/icons/refresh: it clears the icon cache so
// renamed stacks and newly published icons are re-fetched on the next request.
func RefreshHandler(svc *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := svc.ClearCache(); err != nil {
			http.Error(w, "failed to clear icon cache", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
