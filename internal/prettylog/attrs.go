package prettylog

import "log/slog"

// attr is one flattened key/value pair, in the order it was logged (unlike a
// map, ordering matters for readable console output).
type attr struct {
	key string
	val slog.Value
}

// collectAttrs flattens bound + record attrs into an ordered list,
// group-prefixing keys with dots (mirrors internal/logbuf's convention).
func collectAttrs(bound []slog.Attr, group string, r slog.Record) []attr {
	prefix := ""
	if group != "" {
		prefix = group + "."
	}
	out := make([]attr, 0, len(bound)+r.NumAttrs())
	for _, a := range bound {
		out = appendAttr(out, prefix, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		out = appendAttr(out, prefix, a)
		return true
	})
	return out
}

func appendAttr(dst []attr, prefix string, a slog.Attr) []attr {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, ga := range v.Group() {
			dst = appendAttr(dst, prefix+a.Key+".", ga)
		}
		return dst
	}
	return append(dst, attr{key: prefix + a.Key, val: v})
}

// find returns the value bound to key, if any.
func find(attrs []attr, key string) (slog.Value, bool) {
	for _, a := range attrs {
		if a.key == key {
			return a.val, true
		}
	}
	return slog.Value{}, false
}

func hasKey(attrs []attr, key string) bool {
	_, ok := find(attrs, key)
	return ok
}

// str returns the string form of key's value, or "" if absent.
func str(attrs []attr, key string) string {
	v, ok := find(attrs, key)
	if !ok {
		return ""
	}
	return v.String()
}

// strSlice returns key's value as a []string. skipper-cd only ever logs
// []string via slog.Any, so a non-matching kind (or absent key) yields nil.
func strSlice(attrs []attr, key string) []string {
	v, ok := find(attrs, key)
	if !ok {
		return nil
	}
	s, _ := v.Any().([]string)
	return s
}

// intAttr returns key's value as an int64, or 0 if absent or not an integer
// kind (guards against a panic from slog.Value.Int64 on a mismatched kind).
func intAttr(attrs []attr, key string) int64 {
	v, ok := find(attrs, key)
	if !ok || v.Kind() != slog.KindInt64 {
		return 0
	}
	return v.Int64()
}
