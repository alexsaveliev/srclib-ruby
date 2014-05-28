package golang

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"strings"

	"github.com/golang/gddo/gosrc"
	"sourcegraph.com/sourcegraph"
	"sourcegraph.com/sourcegraph/srcgraph/config"
	"sourcegraph.com/sourcegraph/srcgraph/container"
	"sourcegraph.com/sourcegraph/srcgraph/dep2"
	"sourcegraph.com/sourcegraph/srcgraph/unit"
)

func init() {
	dep2.RegisterLister(&Package{}, dep2.DockerLister{defaultGoVersion})
	dep2.RegisterResolver(goImportPathTargetType, defaultGoVersion)
}

func (v *goVersion) BuildLister(dir string, unit unit.SourceUnit, c *config.Repository) (*container.Command, error) {
	pkg := unit.(*Package)

	cont, err := v.containerForRepo(dir, unit, c)
	if err != nil {
		return nil, err
	}

	cont.Cmd = []string{"go", "list", "-e", "-f", `{{join .Imports "\n"}}` + "\n" + `{{join .TestImports "\n"}}` + "\n" + `{{join .XTestImports "\n"}}`, pkg.ImportPath}

	cmd := container.Command{
		Container: *cont,
		Transform: func(orig []byte) ([]byte, error) {
			importPaths := strings.Split(string(orig), "\n")
			seen := make(map[string]struct{})
			var deps []*dep2.RawDependency
			for _, importPath := range importPaths {
				if importPath == "" {
					continue
				}
				if _, seen := seen[importPath]; seen {
					continue
				}
				seen[importPath] = struct{}{}
				deps = append(deps, &dep2.RawDependency{
					TargetType: goImportPathTargetType,
					Target:     goImportPath(importPath),
				})
			}

			return json.Marshal(deps)
		},
	}
	return &cmd, nil
}

// goImportPath represents a Go import path, such as "github.com/user/repo" or
// "net/http".
type goImportPath string

const goImportPathTargetType = "go-import-path"

func (v *goVersion) Resolve(dep *dep2.RawDependency, c *config.Repository) (*dep2.ResolvedTarget, error) {
	importPath := dep.Target.(string)
	return v.resolveGoImportDep(importPath, c)
}

func (v *goVersion) resolveGoImportDep(importPath string, c *config.Repository) (*dep2.ResolvedTarget, error) {
	// Map code.google.com/p/go to Go stdlib.
	importPath = strings.TrimPrefix(importPath, v.BaseImportPath+"/")

	// Look up in cache.
	resolvedTarget := func() *dep2.ResolvedTarget {
		v.resolveCacheMu.Lock()
		defer v.resolveCacheMu.Unlock()
		return v.resolveCache[importPath]
	}()
	if resolvedTarget != nil {
		return resolvedTarget, nil
	}

	// Check if this importPath is in this repository.
	goConfig := v.goConfig(c)
	if strings.HasPrefix(importPath, goConfig.BaseImportPath) {
		dir, err := filepath.Rel(goConfig.BaseImportPath, importPath)
		if err != nil {
			return nil, err
		}
		toUnit := &Package{Dir: dir, ImportPath: importPath}
		return &dep2.ResolvedTarget{
			// empty ToRepoCloneURL to indicate it's from this repository
			ToRepoCloneURL: "",
			ToUnit:         toUnit.Name(),
			ToUnitType:     unit.Type(toUnit),
		}, nil
	}

	// Special-case the cgo package "C".
	if importPath == "C" {
		return nil, nil
	}

	if gosrc.IsGoRepoPath(importPath) || importPath == "debug/goobj" || importPath == "debug/plan9obj" {
		toUnit := &Package{ImportPath: importPath, Dir: "src/pkg/" + importPath}
		return &dep2.ResolvedTarget{
			ToRepoCloneURL:  v.RepositoryCloneURL,
			ToVersionString: v.VersionString,
			ToRevSpec:       v.VCSRevision,
			ToUnit:          toUnit.Name(),
			ToUnitType:      unit.Type(toUnit),
		}, nil
	}

	log.Printf("Resolving Go dep: %s", importPath)

	dir, err := gosrc.Get(sourcegraph.AuthenticatingAsNeededHTTPClient, string(importPath), "")
	if err != nil {
		return nil, fmt.Errorf("unable to fetch information about Go package %q: %s", importPath, err)
	}

	// gosrc returns code.google.com URLs ending in a slash. Remove it.
	dir.ProjectURL = strings.TrimSuffix(dir.ProjectURL, "/")

	toUnit := &Package{ImportPath: dir.ImportPath}
	toUnit.Dir, err = filepath.Rel(dir.ProjectRoot, dir.ImportPath)
	if err != nil {
		return nil, err
	}

	resolvedTarget = &dep2.ResolvedTarget{
		ToRepoCloneURL: dir.ProjectURL,
		ToUnit:         toUnit.Name(),
		ToUnitType:     unit.Type(toUnit),
	}

	if gosrc.IsGoRepoPath(dir.ImportPath) {
		resolvedTarget.ToVersionString = v.VersionString
		resolvedTarget.ToRevSpec = v.VCSRevision
		resolvedTarget.ToUnit = resolvedTarget.ToUnit
	}

	// Save in cache.
	v.resolveCacheMu.Lock()
	defer v.resolveCacheMu.Unlock()
	if v.resolveCache == nil {
		v.resolveCache = make(map[string]*dep2.ResolvedTarget)
	}
	v.resolveCache[importPath] = resolvedTarget

	return resolvedTarget, nil
}
