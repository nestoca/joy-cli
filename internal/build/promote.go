package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/kyokomi/emoji"
	"github.com/nestoca/joy-cli/internal/utils"
	"gopkg.in/yaml.v3"
)

type PromoteArgs struct {
	Environment string
	Project     string
	Version     string
	CatalogDir  string
}

type promoteTarget struct {
	File    os.FileInfo
	Path    string
	Release interface{}
}

func Promote(args PromoteArgs) error {
	envReleasesDir := filepath.Join(args.CatalogDir, "environments", args.Environment, "releases")

	var targets []*promoteTarget

	err := filepath.Walk(envReleasesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(info.Name(), ".release.yaml") {
			return nil
		}

		file, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading release file: %w", err)
		}

		var release interface{}
		err = yaml.Unmarshal(file, &release)
		if err != nil {
			return fmt.Errorf("parsing release file: %w", err)
		}

		releaseProject, err := utils.TraverseYAML(release, ".spec.project")
		if err != nil {
			return fmt.Errorf("reading release's project: %w", err)
		}

		if releaseProject != nil && args.Project == releaseProject.(string) {
			targets = append(targets, &promoteTarget{
				File:    info,
				Release: release,
				Path:    path,
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking catalog directory: %w", err)
	}

	for _, target := range targets {
		err := utils.SetYAMLValue(target.Release, ".spec.version", args.Version)
		if err != nil {
			return fmt.Errorf("updating release version: %w", err)
		}

		result, err := yaml.Marshal(target.Release)
		if err != nil {
			return fmt.Errorf("marshalling updated release: %w", err)
		}
		err = os.WriteFile(target.Path, result, target.File.Mode())
		if err != nil {
			return fmt.Errorf("writing to release file: %w", err)
		}

		releaseName, err := utils.TraverseYAML(target.Release, ".metadata.name")
		if err != nil {
			return fmt.Errorf("reading release's name: %w", err)
		}

		_, _ = emoji.Printf(":check_mark:Promoted release %s to version %s\n", color.HiBlueString(releaseName.(string)), color.GreenString(args.Version))
	}

	if len(targets) > 0 {
		_, _ = emoji.Printf("\n:beer:Done! Promoted releases of project %s in environment %s to version %s\n", color.HiCyanString(args.Project), color.HiCyanString(args.Environment), color.GreenString(args.Version))
	} else {
		_, _ = emoji.Printf(":warning:Did not find any releases for project %s\n", color.HiYellowString(args.Project))
	}

	return nil
}
