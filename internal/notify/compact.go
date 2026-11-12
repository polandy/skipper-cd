package notify

import (
	"fmt"
	"strings"

	"github.com/polandy/skipper-cd/internal/events"
)

// Compact deploy-message wording. Kept in one place so the language can be
// changed without touching the composition logic.
const (
	wordUpdated        = "aktualisiert"
	wordRebuilt        = "neu gebaut"
	wordRedeployed     = "neu deployt"
	wordRemoved        = "entfernt"
	wordDeployFailed   = "Deploy fehlgeschlagen"
	wordBackOn         = "zurück auf"
	wordRolledBack     = "zurückgerollt"
	wordRollbackSick   = "Rollback ebenfalls ungesund"
	wordHealGaveUp     = "Selbstheilung aufgegeben"
	githubReleasesFmt  = "https://github.com/%s/releases/tag/%s"
	ghcrPrefix         = "ghcr.io/"
	digestShortLen     = 7
	arrow              = " → "
	changeKindTag      = "tag"
	changeKindDigest   = "digest"
	changeKindImage    = "image"
	changeKindAdded    = "added"
	changeKindRemoved  = "removed"
	groupNameSeparator = ", "
)

// compactMessage is a deploy outcome reduced to what a phone notification
// needs: a title that carries the point, a short body, and an optional link.
type compactMessage struct {
	Title   string
	Body    string
	Click   string // release notes of the new tag, "" when not derivable
	Success bool
}

// serviceChange is one ServiceImageChange classified for display.
type serviceChange struct {
	kind     string
	name     string // service name, with " (suffix)" when its tag carries one
	from, to string // what the line shows after the name; "" for none
	tag      string // old tag (tag/digest kinds): what a rollback returns to
	newTag   string
	newRepo  string
}

// composeDeploy builds the compact message for a terminal deploy event.
func composeDeploy(ev events.DeployEvent) compactMessage {
	changes := classifyChanges(ev.ImageChanges)
	switch ev.Status {
	case events.StatusSuccess:
		m := composeSuccess(ev.Stack, changes)
		m.Success = true
		return m
	case events.StatusHealExhausted:
		return compactMessage{Title: ev.Stack + ": " + wordHealGaveUp, Body: firstLine(ev.Error)}
	case events.StatusRolledBack:
		title := ev.Stack + ": " + wordDeployFailed + ", "
		if tag := commonOldTag(changes); tag != "" {
			title += wordBackOn + " " + tag
		} else {
			title += wordRolledBack
		}
		return compactMessage{Title: title, Body: firstLine(ev.Error)}
	case events.StatusRolledBackUnhealthy:
		return compactMessage{Title: ev.Stack + ": " + wordDeployFailed + ", " + wordRollbackSick, Body: firstLine(ev.Error)}
	case events.StatusFailed:
		return compactMessage{Title: ev.Stack + ": " + wordDeployFailed, Body: firstLine(ev.Error)}
	default:
		return compactMessage{Title: fmt.Sprintf("deploy %s: %s", ev.Status, ev.Stack), Body: firstLine(ev.Error)}
	}
}

func composeSuccess(stack string, changes []serviceChange) compactMessage {
	if len(changes) == 0 {
		return compactMessage{Title: stack + " " + wordRedeployed}
	}
	groups := groupChanges(changes)
	if allOfKind(changes, changeKindTag) && len(groups) == 1 {
		g := groups[0]
		return compactMessage{
			Title: stack + ": " + g.from + arrow + g.to,
			Body:  joinNames(g.names),
			Click: releaseURL(changes[0]),
		}
	}
	if allOfKind(changes, changeKindDigest) && changes[0].tag != "" && sameTag(changes) {
		return compactMessage{Title: stack + ": " + changes[0].tag + " " + wordRebuilt, Body: renderGroups(groups)}
	}
	return compactMessage{Title: stack + " " + wordUpdated, Body: renderGroups(groups)}
}

// classifyChanges turns image changes into display rows: tags reduced to the
// differing version (a shared suffix such as "-openvino" moves to the name),
// digests shortened, registry and repository dropped.
func classifyChanges(in []events.ServiceImageChange) []serviceChange {
	out := make([]serviceChange, 0, len(in))
	for _, c := range in {
		switch {
		case c.New == "":
			out = append(out, serviceChange{kind: changeKindRemoved, name: c.Service, to: wordRemoved})
		case c.Old == "":
			out = append(out, serviceChange{kind: changeKindAdded, name: c.Service, to: displayTag(c.New)})
		case imageRepository(c.Old) != imageRepository(c.New):
			out = append(out, serviceChange{kind: changeKindImage, name: c.Service, from: c.Old, to: c.New})
		case imageTag(c.Old) != imageTag(c.New):
			oc, nc, suffix := splitTagSuffix(imageTag(c.Old), imageTag(c.New))
			out = append(out, serviceChange{
				kind: changeKindTag, name: withSuffix(c.Service, suffix), from: oc, to: nc,
				tag: oc, newTag: nc, newRepo: imageRepository(c.New),
			})
		default:
			out = append(out, serviceChange{
				kind: changeKindDigest, name: c.Service, from: shortDigest(c.Old), to: shortDigest(c.New),
				tag: imageTag(c.Old),
			})
		}
	}
	return out
}

// splitTagSuffix drops a "-suffix" both tags share ("v3.4.0-openvino" →
// "v3.4.1-openvino" gives "v3.4.0", "v3.4.1", "openvino"). Tags that share no
// such suffix, or would be left empty by it, come back unchanged.
func splitTagSuffix(oldTag, newTag string) (string, string, string) {
	n := 0
	for n < len(oldTag) && n < len(newTag) && oldTag[len(oldTag)-1-n] == newTag[len(newTag)-1-n] {
		n++
	}
	common := oldTag[len(oldTag)-n:]
	dash := strings.IndexByte(common, '-')
	if dash == -1 {
		return oldTag, newTag, ""
	}
	cut := len(common) - dash
	oc, nc := oldTag[:len(oldTag)-cut], newTag[:len(newTag)-cut]
	if oc == "" || nc == "" || oc == nc {
		return oldTag, newTag, ""
	}
	return oc, nc, common[dash+1:]
}

func withSuffix(name, suffix string) string {
	if suffix == "" {
		return name
	}
	return name + " (" + suffix + ")"
}

// displayTag is an image reference reduced to its tag, or the reference when
// it has none.
func displayTag(ref string) string {
	if t := imageTag(ref); t != "" {
		return t
	}
	return imageRepository(ref)
}

// shortDigest is the digest or image id behind a reference's "@", cut to
// digestShortLen characters; "" when the reference carries none.
func shortDigest(ref string) string {
	at := strings.IndexByte(ref, '@')
	if at == -1 {
		return ""
	}
	id := strings.TrimPrefix(ref[at+1:], "sha256:")
	if len(id) > digestShortLen {
		id = id[:digestShortLen]
	}
	return id
}

type changeGroup struct {
	from, to string
	names    []string
}

// groupChanges merges changes with the same kind and from → to into one
// group, in order of first appearance.
func groupChanges(changes []serviceChange) []changeGroup {
	var groups []changeGroup
	index := map[string]int{}
	for _, c := range changes {
		key := c.kind + "\x00" + c.from + "\x00" + c.to
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, changeGroup{from: c.from, to: c.to})
		}
		groups[i].names = append(groups[i].names, c.name)
	}
	return groups
}

func renderGroups(groups []changeGroup) string {
	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		line := joinNames(g.names)
		switch {
		case g.from != "" && g.to != "":
			line += ": " + g.from + arrow + g.to
		case g.to != "":
			line += ": " + g.to
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func joinNames(names []string) string { return strings.Join(names, groupNameSeparator) }

func allOfKind(changes []serviceChange, kind string) bool {
	for _, c := range changes {
		if c.kind != kind {
			return false
		}
	}
	return true
}

func sameTag(changes []serviceChange) bool {
	for _, c := range changes {
		if c.tag != changes[0].tag {
			return false
		}
	}
	return true
}

// commonOldTag is the tag every tag/digest change started from, or "" when
// they started from different ones (or there is nothing to name).
func commonOldTag(changes []serviceChange) string {
	tag := ""
	for _, c := range changes {
		if c.kind != changeKindTag && c.kind != changeKindDigest {
			continue
		}
		if tag != "" && c.tag != tag {
			return ""
		}
		tag = c.tag
	}
	return tag
}

// releaseURL links the release notes of a ghcr.io image's new tag, assuming
// the package is named like its repository; "" for any other registry.
func releaseURL(c serviceChange) string {
	path, ok := strings.CutPrefix(c.newRepo, ghcrPrefix)
	if !ok || strings.Count(path, "/") != 1 {
		return ""
	}
	return fmt.Sprintf(githubReleasesFmt, path, c.newTag)
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
