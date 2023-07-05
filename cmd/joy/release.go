package main

import (
	"github.com/nestoca/joy-cli/internal/releasing"
	"github.com/nestoca/joy-cli/internal/releasing/list"
	"github.com/nestoca/joy-cli/internal/releasing/promotion"
	"github.com/spf13/cobra"
)

func NewReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "release",
		Aliases: []string{"releases", "rel"},
		Short:   "Manage releases",
		Long:    `Manage releases, such as promoting a release in a given environment.`,
		GroupID: "core",
	}
	cmd.AddCommand(NewReleaseListCmd())
	cmd.AddCommand(NewReleasePromoteCmd())
	return cmd
}

func NewReleaseListCmd() *cobra.Command {
	var releases string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List releases across environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return list.List(list.Opts{})
		},
	}
	cmd.Flags().StringVarP(&releases, "releases", "r", "", "Releases to list (comma-separated with wildcards, defaults to all)")
	return cmd
}

func NewReleasePromoteCmd() *cobra.Command {
	var doPreview, doPromote, noPush bool
	var releases string

	cmd := &cobra.Command{
		Use:     "promote",
		Aliases: []string{"prom"},
		Short:   "Promote releases from one environment to another",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Options
			opts := promotion.Opts{
				SourceEnv: args[0],
				TargetEnv: args[1],
				Push:      !noPush,
			}

			// Action
			if doPreview {
				opts.Action = promotion.ActionPreview
			} else if doPromote {
				opts.Action = promotion.ActionPromote
			}

			// Filter
			if releases != "" {
				opts.Filter = releasing.NewNamePatternFilter(releases)
			}

			return promotion.Prompt(opts)
		},
	}

	cmd.Flags().BoolVar(&doPreview, "preview", false, "Preview changes (don't prompt for action)")
	cmd.Flags().BoolVar(&doPromote, "promote", false, "Promote changes (don't prompt for action)")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Skip pushing changes (only commit locally)")
	cmd.Flags().StringVarP(&releases, "releases", "r", "", "Releases to promote (comma-separated with wildcards, defaults to prompting user)")

	return cmd
}
