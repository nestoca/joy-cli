package list

import (
	"fmt"
	"github.com/nestoca/joy-cli/internal/environment"
	"github.com/nestoca/joy-cli/internal/git"
	"github.com/nestoca/joy-cli/internal/releasing"
)

type Opts struct {
	// SelectedEnvs is the list of environments that were selected by user to work with.
	SelectedEnvs []string

	// Filter specifies releases to list.
	// Optional, defaults to listing all releases.
	Filter releasing.Filter
}

func List(opts Opts) error {
	err := git.EnsureCleanAndUpToDateWorkingCopy()
	if err != nil {
		return err
	}

	environments, err := environment.LoadAll(environment.DirName, opts.SelectedEnvs...)
	if err != nil {
		return fmt.Errorf("loading environments: %w", err)
	}

	list, err := releasing.LoadCrossReleaseList(environment.DirName, environments, opts.Filter)
	if err != nil {
		return fmt.Errorf("loading cross-environment releases: %w", err)
	}

	list.Print(releasing.PrintOpts{})
	return nil
}
