package promote

import (
	"fmt"
	"strings"

	"github.com/nestoca/joy/internal/release"

	"github.com/nestoca/joy/internal/github"
	"github.com/nestoca/joy/internal/project"

	"github.com/nestoca/joy/api/v1alpha1"
	"github.com/nestoca/joy/internal/git/pr"
	"github.com/nestoca/joy/internal/release/cross"
	"github.com/nestoca/joy/internal/yml"
	"github.com/nestoca/joy/pkg/catalog"
)

type Promotion struct {
	// Prompt is the prompt to use for user interaction.
	promptProvider PromptProvider

	// Committer allows committing and pushing changes to git.
	gitProvider GitProvider

	// PullRequestProvider is the provider of pull requests.
	pullRequestProvider pr.PullRequestProvider

	// YamlWriter is the writer of YAML files.
	yamlWriter YamlWriter

	commitTemplate            string
	pullRequestTemplate       string
	getProjectRepositoryFunc  func(proj *v1alpha1.Project) string
	getProjectSourceDirFunc   func(proj *v1alpha1.Project) (string, error)
	getCommitsMetadataFunc    func(projectDir, from, to string) ([]*CommitMetadata, error)
	getCommitGitHubAuthorFunc func(proj *v1alpha1.Project, sha string) (string, error)
	getReleaseGitTagFunc      func(release *v1alpha1.Release) (string, error)
}

func NewPromotion(
	prompt PromptProvider,
	gitProvider GitProvider,
	pullRequestProvider pr.PullRequestProvider,
	yamlWriter YamlWriter,
	commitTemplate string,
	pullRequestTemplate string,
	getProjectRepositoryFunc func(proj *v1alpha1.Project) string,
	getProjectSourceDirFunc func(proj *v1alpha1.Project) (string, error),
	getCommitsMetadataFunc func(projectDir, from, to string) ([]*CommitMetadata, error),
	getCommitGitHubAuthorFunc func(proj *v1alpha1.Project, sha string) (string, error),
	getReleaseGitTagFunc func(release *v1alpha1.Release) (string, error),
) *Promotion {
	return &Promotion{
		promptProvider:            prompt,
		gitProvider:               gitProvider,
		pullRequestProvider:       pullRequestProvider,
		yamlWriter:                yamlWriter,
		commitTemplate:            commitTemplate,
		pullRequestTemplate:       pullRequestTemplate,
		getProjectRepositoryFunc:  getProjectRepositoryFunc,
		getProjectSourceDirFunc:   getProjectSourceDirFunc,
		getCommitsMetadataFunc:    getCommitsMetadataFunc,
		getCommitGitHubAuthorFunc: getCommitGitHubAuthorFunc,
		getReleaseGitTagFunc:      getReleaseGitTagFunc,
	}
}

func NewDefaultPromotion(catalogDir, gitHubOrganization, commitTemplate, pullRequestTemplate, repositoriesDir, joyCache, defaultGitTagTemplate string) *Promotion {
	return NewPromotion(
		&InteractivePromptProvider{},
		NewShellGitProvider(catalogDir),
		github.NewPullRequestProvider(catalogDir),
		&FileSystemYamlWriter{},
		commitTemplate,
		pullRequestTemplate,
		func(proj *v1alpha1.Project) string {
			if proj.Spec.Repository != "" {
				return proj.Spec.Repository
			}
			return fmt.Sprintf("%s/%s", gitHubOrganization, proj.Name)
		},
		func(proj *v1alpha1.Project) (string, error) {
			return project.GetCachedSourceDir(proj, gitHubOrganization, repositoriesDir, joyCache)
		},
		func(projectDir, from, to string) ([]*CommitMetadata, error) {
			return GetCommitsMetadata(projectDir, from, to)
		},
		func(proj *v1alpha1.Project, sha string) (string, error) {
			return github.GetCommitGitHubAuthor(proj, gitHubOrganization, sha)
		},
		func(rel *v1alpha1.Release) (string, error) {
			return release.GetGitTag(rel, defaultGitTagTemplate)
		},
	)
}

type Opts struct {
	// Catalog contains candidate environments and releases to promote.
	Catalog *catalog.Catalog

	// SourceEnv is the source environment to promote from.
	SourceEnv *v1alpha1.Environment

	// TargetEnv is the target environment to promote to.
	TargetEnv *v1alpha1.Environment

	// Releases are the already selected releases.
	// If there is more than one, no need to prompt the user to select releases.
	Releases []string

	ReleasesFiltered bool

	// NoPrompt means that the Promote function should avoid interactive prompts at all costs.
	NoPrompt bool

	// AutoMerge indicates if PR created needs the auto-merge label
	AutoMerge bool

	// Draft indicates if PR created needs to be draft
	Draft bool

	// SelectedEnvironments is the list of environments selected by the user interactively via `joy env select`.
	SelectedEnvironments []*v1alpha1.Environment

	// DryRun indicates if the promotion should be performed in dry-run mode
	DryRun bool
}

// Promote prompts user to select source and target environments and releases to promote and creates a pull request,
// returning its URL if any.
func (p *Promotion) Promote(opts Opts) (string, error) {
	if opts.DryRun {
		fmt.Println("ℹ️ Dry-run mode enabled: No changes will be made.")
	}

	if err := p.gitProvider.EnsureCleanAndUpToDateWorkingCopy(); err != nil {
		return "", err
	}

	// Prompt user to select source environment
	if opts.SourceEnv == nil {
		sourceEnvs, err := getSourceEnvironments(opts.SelectedEnvironments)
		if err != nil {
			return "", err
		}
		opts.SourceEnv, err = p.promptProvider.SelectSourceEnvironment(sourceEnvs)
		if err != nil {
			return "", err
		}
	}

	// Prompt user to select target environment
	if opts.TargetEnv == nil {
		targetEnvs, err := getTargetEnvironments(opts.SelectedEnvironments, opts.SourceEnv)
		if err != nil {
			return "", err
		}
		opts.TargetEnv, err = p.promptProvider.SelectTargetEnvironment(targetEnvs)
		if err != nil {
			return "", err
		}
	}

	if !opts.TargetEnv.Spec.Promotion.AllowAutoMerge && opts.AutoMerge {
		return "", fmt.Errorf("auto-merge is not allowed for target environment %s", opts.TargetEnv.Name)
	}

	// Validate promotability (only relevant if either or both environments were specified via command line flags)
	if !opts.SourceEnv.IsPromotableTo(opts.TargetEnv) {
		return "", fmt.Errorf("environment %s is not promotable to %s", opts.SourceEnv.Name, opts.TargetEnv.Name)
	}

	list, err := opts.Catalog.Releases.GetReleasesForPromotion(opts.SourceEnv, opts.TargetEnv)
	if err != nil {
		return "", fmt.Errorf("getting releases for promotion: %w", err)
	}

	if !list.HasAnyPromotableReleases() {
		p.promptProvider.PrintNoPromotableReleasesFound(opts.ReleasesFiltered, opts.SourceEnv, opts.TargetEnv)
		return "", nil
	}

	selectedList, err := func() (*cross.ReleaseList, error) {
		if len(opts.Releases) > 0 {
			return list.OnlySpecificReleases(opts.Releases), nil
		}
		return p.promptProvider.SelectReleases(list)
	}()
	if err != nil {
		return "", fmt.Errorf("selecting releases to promote: %w", err)
	}

	invalidList := selectedList.GetNonPromotableReleases(opts.SourceEnv, opts.TargetEnv)
	if len(invalidList) != 0 {
		invalid := strings.Join(invalidList, ", ")
		p.promptProvider.PrintSelectedNonPromotableReleases(invalid, opts.TargetEnv.Name)
		return "", fmt.Errorf("cannot promote releases with non-standard version to %s environment", opts.TargetEnv.Name)
	}

	if !opts.NoPrompt {
		if err := p.preview(selectedList); err != nil {
			return "", fmt.Errorf("previewing: %w", err)
		}
	}

	// There's a previous check so only one option can be true at a time
	performParams := PerformOpts{
		list:                      selectedList,
		autoMerge:                 opts.AutoMerge,
		draft:                     opts.Draft,
		dryRun:                    opts.DryRun,
		commitTemplate:            p.commitTemplate,
		pullRequestTemplate:       p.pullRequestTemplate,
		getProjectSourceDirFunc:   p.getProjectSourceDirFunc,
		getProjectRepositoryFunc:  p.getProjectRepositoryFunc,
		getCommitsMetadataFunc:    p.getCommitsMetadataFunc,
		getCommitGitHubAuthorFunc: p.getCommitGitHubAuthorFunc,
		getReleaseGitTagFunc:      p.getReleaseGitTagFunc,
	}

	if opts.NoPrompt {
		return p.perform(performParams)
	}

	if opts.AutoMerge || opts.Draft {
		confirmed, err := p.promptProvider.ConfirmCreatingPromotionPullRequest(opts.AutoMerge, opts.Draft)
		if err != nil {
			return "", fmt.Errorf("confirming creating promotion pull request: %w", err)
		}
		if !confirmed {
			p.promptProvider.PrintCanceled()
			return "", nil
		}

		return p.perform(performParams)
	}

	// Prompt user to select creating a pull request
	answer, err := p.promptProvider.SelectCreatingPromotionPullRequest()
	if err != nil {
		return "", fmt.Errorf("selecting create promotion pull request: %w", err)
	}

	switch answer {
	case Ready:
		if opts.TargetEnv.Spec.Promotion.AllowAutoMerge {
			confirmed, err := p.promptProvider.ConfirmAutoMergePullRequest()
			if err != nil {
				return "", fmt.Errorf("confirming automerge: %w", err)
			}
			performParams.autoMerge = confirmed
		}
	case Draft:
		performParams.draft = true
	case Cancel:
		p.promptProvider.PrintCanceled()
		return "", nil
	}

	return p.perform(performParams)
}

func (p *Promotion) preview(list *cross.ReleaseList) error {
	p.promptProvider.PrintStartPreview()
	targetEnv := list.Environments[1]

	for _, rel := range list.Items {
		// Skip releases that are already in sync
		if rel.PromotedFile == nil {
			continue
		}

		targetRelease := rel.Releases[1]
		var targetReleaseFile *yml.File
		if targetRelease != nil {
			targetReleaseFile = targetRelease.File
		}
		err := p.promptProvider.PrintReleasePreview(targetEnv.Name, rel.Name, targetReleaseFile, rel.PromotedFile)
		if err != nil {
			return fmt.Errorf("printing release preview: %w", err)
		}
	}

	p.promptProvider.PrintEndPreview()
	return nil
}

func getSourceEnvironments(environments []*v1alpha1.Environment) ([]*v1alpha1.Environment, error) {
	envsMap := make(map[string]bool)
	for _, env := range environments {
		for _, source := range env.Spec.Promotion.FromEnvironments {
			envsMap[source] = true
		}
	}
	var envs []*v1alpha1.Environment
	for _, env := range environments {
		if envsMap[env.Name] {
			envs = append(envs, env)
		}
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("no promotable source environments found")
	}
	return envs, nil
}

func getTargetEnvironments(environments []*v1alpha1.Environment, sourceEnvironment *v1alpha1.Environment) ([]*v1alpha1.Environment, error) {
	var envs []*v1alpha1.Environment
	for _, env := range environments {
		if env.Name != sourceEnvironment.Name {
			for _, source := range env.Spec.Promotion.FromEnvironments {
				if source == sourceEnvironment.Name {
					envs = append(envs, env)
				}
			}
		}
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("no target environments found to promote from %s", sourceEnvironment.Name)
	}
	return envs, nil
}
