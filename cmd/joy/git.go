package main

import (
	"fmt"
	"github.com/nestoca/joy/internal/git"
	"github.com/spf13/cobra"
	"os"
)

// changeToCatalogDir changes the current directory to the catalog, for commands
// that need to be run from there.
func changeToCatalogDir() error {
	err := os.Chdir(cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("changing to catalog directory: %w", err)
	}
	return nil
}

func NewGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "git",
		Short:              "Call arbitrary git command against catalog with given arguments",
		Long:               `Call arbitrary git command against catalog with given arguments`,
		GroupID:            "git",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeToCatalogDir(); err != nil {
				return err
			}
			return git.Run(args)
		},
	}
	return cmd
}

func NewPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "pull",
		Short:              "Pull catalog changes from git remote",
		GroupID:            "git",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeToCatalogDir(); err != nil {
				return err
			}
			return git.Pull(args...)
		},
	}
	return cmd
}

func NewPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "push",
		Short:              "Push catalog changes to git remote",
		GroupID:            "git",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeToCatalogDir(); err != nil {
				return err
			}
			return git.Push(args...)
		},
	}
	return cmd
}

func NewResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reset",
		Short:   "Reset all uncommitted catalog changes",
		GroupID: "git",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeToCatalogDir(); err != nil {
				return err
			}
			return git.Reset()
		},
	}
	return cmd
}
