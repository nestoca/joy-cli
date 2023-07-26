package promote

import (
	"fmt"
	"github.com/TwiN/go-color"
	"github.com/nestoca/joy/internal/catalog"
	"github.com/nestoca/joy/internal/yml"
)

type Opts struct {
	Environment string
	Project     string
	Version     string
}

func Promote(opts Opts) error {
	loadOpts := catalog.LoadOpts{
		LoadEnvs:     true,
		LoadReleases: true,
		EnvNames:     []string{opts.Environment},
	}
	cat, err := catalog.Load(loadOpts)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	promotionCount := 0
	for _, crossRelease := range cat.CrossReleases.Items {
		release := crossRelease.Releases[0]
		if release.Spec.Project == opts.Project {
			// Find version node
			versionNode, err := yml.FindNode(release.File.Tree, "spec.version")
			if err != nil {
				return fmt.Errorf("release %s has no version property: %w", release.Metadata.Name, err)
			}

			// Update version node
			versionNode.Value = opts.Version
			err = release.File.UpdateYamlFromTree()
			if err != nil {
				return fmt.Errorf("updating release yaml from node tree: %w", err)
			}

			// Write release file back
			err = release.File.WriteYaml()
			if err != nil {
				return fmt.Errorf("writing release file: %w", err)
			}
			fmt.Printf("✅ Promoted release %s to version %s\n", color.InGreen(release.Name), color.InYellow(opts.Version))
			promotionCount++
		}
	}

	// Print summary
	if promotionCount == 0 {
		return fmt.Errorf("no releases found for project %s", opts.Project)
	}
	plural := ""
	if promotionCount > 1 {
		plural = "s"
	}
	fmt.Printf("🍺 Promoted %d release%s of project %s in environment %s to version %s\n", promotionCount, plural, color.InGreen(opts.Project), color.InGreen(opts.Environment), color.InYellow(opts.Version))
	return nil
}
