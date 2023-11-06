package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nestoca/joy/internal/config"
	"github.com/nestoca/joy/internal/dependencies"
)

var (
	cfg                   *config.Config
	configDir, catalogDir string
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func NewRootCmd() *cobra.Command {
	setupCmd := NewSetupCmd()

	cmd := &cobra.Command{
		Use:          "joy",
		Short:        "Manages project, environment and release resources as code",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if cmd != setupCmd {
				if err := dependencies.AllRequiredMustBeInstalled(); err != nil {
					return err
				}
			}

			cfg, err = config.Load(configDir, catalogDir)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cmd != setupCmd {
				err = config.CheckCatalogDir(cfg.CatalogDir)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "Directory containing .joyrc config file (defaults to $HOME)")
	cmd.PersistentFlags().StringVar(&catalogDir, "catalog-dir", "", "Directory containing joy catalog of environments, projects and releases (defaults to $HOME/.joy)")

	// Core commands
	cmd.AddGroup(&cobra.Group{ID: "core", Title: "Core commands"})
	cmd.AddCommand(NewEnvironmentCmd())
	cmd.AddCommand(NewReleaseCmd())
	cmd.AddCommand(NewProjectCmd())
	cmd.AddCommand(NewPRCmd())
	cmd.AddCommand(NewBuildCmd())

	// Catalog git commands
	cmd.AddGroup(&cobra.Group{ID: "git", Title: "Catalog git commands"})
	cmd.AddCommand(NewGitCmd())
	cmd.AddCommand(NewPullCmd())
	cmd.AddCommand(NewPushCmd())
	cmd.AddCommand(NewResetCmd())

	// Additional commands
	cmd.AddCommand(NewSecretCmd())
	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(setupCmd)

	return cmd
}
