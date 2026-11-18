// Failure cause: the few stderr lines that say why a command failed, condensed
// into the deploy error so the event, the notification and the audit log name
// the cause instead of only "exit status 1" (ADR-0063).

package deploy

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/polandy/skipper-cd/internal/command"
)

const (
	// maxCausePart caps each line the cause is built from.
	maxCausePart = 160
	// maxCauseCommand caps the command BuildKit quotes in its verdict
	// (`process "/bin/sh -c …"`): its start identifies it, the rest is noise.
	maxCauseCommand = 60
	// causeSeparator joins the process's own error line to BuildKit's verdict.
	causeSeparator = " — "
)

var (
	// ansiEscape matches a CSI escape sequence (colours, cursor movement).
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// buildkitStep matches the step prefix of BuildKit's plain progress
	// output ("#6 "), capturing the step number.
	buildkitStep = regexp.MustCompile(`^#(\d+) `)
	// elapsedPrefix matches the seconds-since-step-start stamp BuildKit puts
	// before a step's output ("4.640 "). It differs on every run, so it must
	// never reach the error text: ADR-0056 collapses a repeat only when the
	// text is identical.
	elapsedPrefix = regexp.MustCompile(`^\d+\.\d+ `)
	// errorLine matches a line that announces an error in the usual spellings
	// (apt's "E:", "ERROR", "Error response from daemon", "fatal:", …).
	errorLine = regexp.MustCompile(`^(E:|ERROR|Error|error|fatal|FATAL|panic:|npm ERR!|failed to solve)`)
	// quotedCommand matches the command inside BuildKit's verdict.
	quotedCommand = regexp.MustCompile(`process "(.*)" did not complete`)
)

// buildkitVerdict is how BuildKit prefixes its one-line verdict on a failed
// step, after the step number: "#6 ERROR: process … exit code: 100".
const buildkitVerdict = "ERROR: "

// causeLine is one stderr line with its BuildKit step (empty when the line
// carries none) split off.
type causeLine struct {
	step string
	text string
}

// withFailureCause appends the cause a failed command's stderr names to its
// error. An error that carries no stderr tail (a fake runner, a command that
// printed nothing) is returned unchanged.
func withFailureCause(err error) error {
	cause := failureCause(command.StderrTail(err))
	if cause == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, cause)
}

// failureCause condenses a failed command's last stderr lines into one line.
// For a failed BuildKit step it is the step's first error line plus BuildKit's
// verdict; otherwise the last line that announces an error, else the last
// line. Escape sequences, progress redraws and per-run timestamps are removed,
// so the same failure always yields the same text. "" when nothing is left.
func failureCause(tail []string) string {
	lines := normalizeCauseLines(tail)
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l.step == "" || !strings.HasPrefix(l.text, buildkitVerdict) {
			continue
		}
		verdict := shortenCause(shortenQuotedCommand(strings.TrimPrefix(l.text, buildkitVerdict)))
		if first := firstStepError(lines[:i], l.step); first != "" {
			return shortenCause(first) + causeSeparator + verdict
		}
		return verdict
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if errorLine.MatchString(lines[i].text) {
			return shortenCause(lines[i].text)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return shortenCause(lines[len(lines)-1].text)
}

// normalizeCauseLines strips escape sequences, all but the last redraw of a
// carriage-return progress line, the BuildKit step and elapsed-time prefixes,
// and drops lines left empty or made only of dashes (BuildKit's excerpt
// frames).
func normalizeCauseLines(tail []string) []causeLine {
	out := make([]causeLine, 0, len(tail))
	for _, raw := range tail {
		s := ansiEscape.ReplaceAllString(raw, "")
		if i := strings.LastIndexByte(s, '\r'); i >= 0 {
			s = s[i+1:]
		}
		var step string
		if m := buildkitStep.FindStringSubmatch(s); m != nil {
			step, s = m[1], s[len(m[0]):]
		}
		s = strings.TrimSpace(elapsedPrefix.ReplaceAllString(s, ""))
		if strings.Trim(s, "-") == "" {
			continue
		}
		out = append(out, causeLine{step: step, text: s})
	}
	return out
}

// firstStepError returns the first error line the given BuildKit step's own
// process printed, or "". The first one, not the last: tools such as apt
// state the problem first and follow it with "additional context" lines that
// match the same pattern.
func firstStepError(lines []causeLine, step string) string {
	for _, l := range lines {
		if l.step == step && errorLine.MatchString(l.text) {
			return l.text
		}
	}
	return ""
}

// shortenQuotedCommand cuts the command BuildKit quotes in its verdict to
// maxCauseCommand runes.
func shortenQuotedCommand(s string) string {
	m := quotedCommand.FindStringSubmatchIndex(s)
	if m == nil {
		return s
	}
	return s[:m[2]] + truncateRunes(s[m[2]:m[3]], maxCauseCommand) + s[m[3]:]
}

// shortenCause cuts one cause line to maxCausePart runes.
func shortenCause(s string) string {
	return truncateRunes(s, maxCausePart)
}

// truncateRunes cuts s to at most n runes, marking a cut with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
