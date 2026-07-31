package deploy

import (
	"bufio"
	"bytes"
	"encoding/json"
)

// composeJSONLineCap bounds one line of a `--format json` read. Compose emits
// one object per line, so this only has to hold a single record.
const composeJSONLineCap = 1024 * 1024

// parseComposeJSON parses the output of a `docker compose … --format json` read
// into T. Compose emits either a JSON array or newline-delimited objects
// depending on its version, so both are accepted. Empty output (nothing to
// report) yields no lines and no error.
func parseComposeJSON[T any](out []byte) ([]T, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var lines []T
		if err := json.Unmarshal(trimmed, &lines); err != nil {
			return nil, err
		}
		return lines, nil
	}

	var lines []T
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 0, 64*1024), composeJSONLineCap)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var line T
		if err := json.Unmarshal(b, &line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}
