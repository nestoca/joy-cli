package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/sprig/v3"

	"github.com/TwiN/go-color"
	"github.com/nestoca/survey/v2"
	"gopkg.in/yaml.v3"

	"github.com/nestoca/joy/api/v1alpha1"
	"github.com/nestoca/joy/internal"
	"github.com/nestoca/joy/internal/config"
	"github.com/nestoca/joy/internal/environment"
	"github.com/nestoca/joy/internal/helm"
	"github.com/nestoca/joy/internal/release/cross"
	"github.com/nestoca/joy/pkg/catalog"
)

type RenderParams struct {
	Env     string
	Release string
	Cache   helm.ChartCache
	Catalog *catalog.Catalog
	CommonRenderParams
}

type CommonRenderParams struct {
	ValueMapping *config.ValueMapping
	IO           internal.IO
	Helm         helm.PullRenderer
	Color        bool
}

func Render(ctx context.Context, params RenderParams) error {
	environment, err := getEnvironment(params.Catalog.Environments, params.Env)
	if err != nil {
		return fmt.Errorf("getting environment: %w", err)
	}

	release, err := getRelease(params.Catalog.Releases.Items, params.Release, environment.Name)
	if err != nil {
		return fmt.Errorf("getting release: %w", err)
	}

	chart, err := params.Cache.GetReleaseChartFS(ctx, release)
	if err != nil {
		return fmt.Errorf("getting release chart: %w", err)
	}

	return RenderRelease(ctx, RenderReleaseParams{
		Release:            release,
		Chart:              chart,
		CommonRenderParams: params.CommonRenderParams,
	})
}

type RenderReleaseParams struct {
	Release *v1alpha1.Release
	Chart   *helm.ChartFS
	CommonRenderParams
}

func RenderRelease(ctx context.Context, params RenderReleaseParams) error {
	values, err := HydrateValues(params.Release, params.ValueMapping)
	if err != nil {
		return fmt.Errorf("hydrating values: %w", err)
	}

	dst := params.IO.Out
	if params.Color {
		dst = ManifestColorWriter{dst}
	}

	opts := helm.RenderOpts{
		Dst:         dst,
		ReleaseName: params.Release.Name,
		ChartPath:   params.Chart.DirName(),
		Values:      values,
	}

	if err := params.Helm.Render(ctx, opts); err != nil {
		return fmt.Errorf("rendering chart: %w", err)
	}

	return nil
}

func getEnvironment(environments []*v1alpha1.Environment, name string) (*v1alpha1.Environment, error) {
	if name == "" {
		return environment.SelectSingle(environments, nil, "Select environment")
	}

	selectedEnv := environment.FindByName(environments, name)
	if selectedEnv == nil {
		return nil, NotFoundError(fmt.Sprintf("not found: %s", name))
	}

	return selectedEnv, nil
}

func getRelease(releases []*cross.Release, name, env string) (*v1alpha1.Release, error) {
	if name == "" {
		return getReleaseViaPrompt(releases, env)
	}

	for _, crossRelease := range releases {
		if crossRelease.Name != name {
			continue
		}
		for _, release := range crossRelease.Releases {
			if release == nil {
				continue
			}
			if release.Environment.Name == env {
				return release, nil
			}
		}
		return nil, NotFoundError(fmt.Sprintf("not found within environment %s: %s", env, name))
	}

	return nil, NotFoundError(fmt.Sprintf("not found: %s", name))
}

func getReleaseViaPrompt(releases []*cross.Release, env string) (*v1alpha1.Release, error) {
	var (
		candidateNames    []string
		candidateReleases []*v1alpha1.Release
	)

	for _, crossRelease := range releases {
		for _, release := range crossRelease.Releases {
			if release == nil {
				continue
			}
			if release.Environment.Name == env {
				candidateNames = append(candidateNames, release.Name)
				candidateReleases = append(candidateReleases, release)
				break
			}
		}
	}

	var idx int
	if err := survey.AskOne(&survey.Select{Message: "Select release", Options: candidateNames, PageSize: 20}, &idx); err != nil {
		return nil, fmt.Errorf("failed prompt: %w", err)
	}

	return candidateReleases[idx], nil
}

func HydrateValues(release *v1alpha1.Release, mappings *config.ValueMapping) (map[string]any, error) {
	params := struct {
		Release     *v1alpha1.Release
		Environment *v1alpha1.Environment
	}{
		release,
		release.Environment,
	}

	// The following call has the side effect of making a deep copy of the values, which is necessary
	// for subsequent step to mutate the copy without affecting the original values.
	values, err := hydrateObjectValues(release.Spec.Values, params.Environment.Spec.Values)
	if err != nil {
		return nil, fmt.Errorf("hydrating object values: %w", err)
	}

	if mappings != nil && !slices.Contains(mappings.ReleaseIgnoreList, release.Name) {
		for mapping, value := range mappings.Mappings {
			setInMap(values, splitIntoPathSegments(mapping), value)
		}
	}

	data, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("").Funcs(sprig.FuncMap()).Parse(string(data))
	if err != nil {
		return nil, err
	}

	var builder bytes.Buffer
	if err := tmpl.Execute(&builder, params); err != nil {
		return nil, err
	}

	var result map[string]any
	if err := yaml.Unmarshal(builder.Bytes(), &result); err != nil {
		return nil, err
	}

	return result, nil
}

var objectValuesRegex = regexp.MustCompile(`^\s*\$(\w+)\(\s*((\.\w+)+)\s*\)\s*$`)

const objectValuesSupportedPrefix = ".Environment.Spec.Values."

func hydrateObjectValues(values map[string]any, envValues map[string]any) (map[string]any, error) {
	resolvedValue, err := hydrateObjectValue(values, envValues)
	if err != nil {
		return nil, err
	}
	return resolvedValue.(map[string]any), err
}

func hydrateObjectValue(value any, envValues map[string]any) (any, error) {
	switch val := value.(type) {
	case string:
		operator, resolvedValue, err := resolveOperatorAndValue(val, envValues)
		if err != nil {
			return nil, err
		}
		if operator != "" && operator != "ref" {
			return nil, fmt.Errorf("only $ref() operator supported within object: %s", val)
		}
		return resolvedValue, nil
	case map[string]any:
		result := map[string]any{}
		for key, subValue := range val {
			resolvedValue, err := hydrateObjectValue(subValue, envValues)
			if err != nil {
				return nil, err
			}
			result[key] = resolvedValue
		}
		return result, nil
	case map[any]any:
		result := map[string]any{}
		for key, subValue := range val {
			resolvedValue, err := hydrateObjectValue(subValue, envValues)
			if err != nil {
				return nil, err
			}
			result[fmt.Sprint(key)] = resolvedValue
		}
		return result, nil
	case []any:
		var values []any
		for _, subValue := range val {
			switch subVal := subValue.(type) {
			case string:
				operator, resolvedValue, err := resolveOperatorAndValue(subVal, envValues)
				if err != nil {
					return nil, err
				}
				if operator == "spread" {
					resolvedSlice, ok := resolvedValue.([]any)
					if !ok {
						return nil, fmt.Errorf("$spread() operator must resolve to an array, but got: %T", resolvedValue)
					}
					values = append(values, resolvedSlice...)
				} else {
					values = append(values, resolvedValue)
				}
			default:
				resolvedValue, err := hydrateObjectValue(subVal, envValues)
				if err != nil {
					return nil, err
				}
				values = append(values, resolvedValue)
			}
		}
		return values, nil
	default:
		return value, nil
	}
}

func resolveOperatorAndValue(value string, envValues map[string]any) (string, any, error) {
	matches := objectValuesRegex.FindStringSubmatch(value)
	if len(matches) == 0 {
		return "", value, nil
	}

	operator := matches[1]
	if operator != "spread" && operator != "ref" {
		return "", nil, fmt.Errorf("unsupported object interpolation operator %q in expression: %s", operator, value)
	}

	fullPath := matches[2]
	if !strings.HasPrefix(fullPath, objectValuesSupportedPrefix) {
		return "", nil, fmt.Errorf("only %q prefix is supported for object interpolation, but found: %s", objectValuesSupportedPrefix, fullPath)
	}
	valuesPath := strings.Split(strings.TrimPrefix(fullPath, objectValuesSupportedPrefix), ".")
	resolvedValue, err := resolveObjectValue(envValues, valuesPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolving object value for path %q: %w", fullPath, err)
	}
	return operator, resolvedValue, nil
}

func resolveObjectValue(values map[string]any, path []string) (any, error) {
	key := path[0]
	value, ok := values[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in values", key)
	}
	if len(path) == 1 {
		return value, nil
	}
	mapValue, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("value for key %q is not a map", key)
	}
	return resolveObjectValue(mapValue, path[1:])
}

// ManifestColorWriter colorizes helm manifest by searching for document breaks
// and source comments. The implementation is naive and depends on the write buffer
// not breaking lines. In theory this means colorization can fail, however in practice
// it works well enough.
type ManifestColorWriter struct {
	dst io.Writer
}

func (w ManifestColorWriter) Write(data []byte) (int, error) {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "# Source:") {
			lines[i] = color.InYellow(line)
		}
	}

	n, err := w.dst.Write([]byte(strings.Join(lines, "\n")))
	return min(n, len(data)), err
}

// setInMap modifies the map by adding the value to the path defined by segments.
// If the path defined by segments already exists, even if it points to a falsy value, this function does nothing.
// It will not overwrite any existing key/value pairs.
func setInMap(mapping map[string]any, segments []string, value any) {
	for i, key := range segments {
		if i == len(segments)-1 {
			if _, ok := mapping[key]; !ok {
				mapping[key] = value
			}
			return
		}

		subValue, ok := mapping[key]
		if !ok {
			submap := map[string]any{}
			mapping[key] = submap
			mapping = submap
			continue
		}

		submap, ok := subValue.(map[string]any)
		if !ok {
			return
		}
		mapping = submap
	}
}

func splitIntoPathSegments(input string) (result []string) {
	var (
		start   int
		escaped bool
	)

	sanitize := func(value string) string {
		value = strings.ReplaceAll(value, `\.`, ".")
		value = strings.ReplaceAll(value, `\\`, `\`)
		return value
	}

	for i, c := range input {
		switch c {
		case '\\':
			escaped = !escaped
		case '.':
			if escaped {
				continue
			}
			result = append(result, sanitize(input[start:i]))
			escaped = false
			start = i + 1
		default:
			escaped = false
		}
	}

	result = append(result, sanitize(input[start:]))

	return
}

type NotFoundError string

func (err NotFoundError) Error() string { return string(err) }

func (NotFoundError) Is(err error) bool {
	_, ok := err.(NotFoundError)
	return ok
}

func IsNotFoundError(err error) bool {
	var notfound NotFoundError
	return errors.Is(err, notfound)
}
