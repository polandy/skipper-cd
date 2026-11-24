// Unpinned image references: an image: without an @sha256: digest cannot be
// rolled back meaningfully (the old compose file names the same tag, which
// already points at the new image) and hides a moved tag from change detection.
// Advisory only — skipper warns once per distinct finding and deploys as usual.

package deploy

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/polandy/skipper-cd/internal/compose"
	"github.com/polandy/skipper-cd/internal/config"
)

const digestMarker = "@sha256:"

const unpinnedWarningMessage = "image reference not pinned to a digest: a rollback cannot restore the previous image and a moved tag goes undetected — pin it as image: name:tag@sha256:…"

// unpinnedImages returns the registry-pulled services whose image: carries no
// digest. Built services and images a build: service produces are excluded:
// they never come from a registry.
func (cf *composeFile) unpinnedImages() serviceImageByName {
	unpinned := make(serviceImageByName)
	for _, name := range cf.pullableServices() {
		if image := cf.Services[name].Image; !strings.Contains(image, digestMarker) {
			unpinned[name] = image
		}
	}
	return unpinned
}

// describe renders the references as "service=image" pairs sorted by service,
// so an unchanged finding reads identically on every run.
func (images serviceImageByName) describe() string {
	pairs := make([]string, 0, len(images))
	for name, image := range images {
		pairs = append(pairs, name+"="+image)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}

// warnUnpinnedImages logs each stack's unpinned image references once per
// distinct set, and once more when a stack becomes fully pinned. A compose file
// that does not parse is skipped: discovery or the deploy reports it.
func (d *Deployer) warnUnpinnedImages(stacks []config.Stack, stacksBaseDir string) {
	current := make(map[string]bool, len(stacks))
	for _, stack := range stacks {
		cf, err := parseComposeFile(filepath.Join(stacksBaseDir, stack.Name, compose.FileName))
		if err != nil {
			continue
		}
		unpinned := cf.unpinnedImages()
		if len(unpinned) == 0 {
			if d.unpinnedImages.clear(stack.Name) {
				slog.Info("image references now pinned to a digest", "stack", stack.Name)
			}
			continue
		}
		current[stack.Name] = true
		if d.unpinnedImages.record(stack.Name, unpinned.describe()) {
			slog.Warn(unpinnedWarningMessage, "stack", stack.Name, "images", unpinned.describe())
		}
	}
	d.unpinnedImages.forgetOthers(current)
}
