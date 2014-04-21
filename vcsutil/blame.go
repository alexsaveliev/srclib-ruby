package vcsutil

import (
	"os"
	"path/filepath"

	"github.com/sourcegraph/go-blame/blame"
	"sourcegraph.com/sourcegraph/srcgraph/config"
	"sourcegraph.com/sourcegraph/srcgraph/task2"
	"sourcegraph.com/sourcegraph/util"
)

var SkipBlame = util.ParseBool(os.Getenv("SG_SKIP_BLAME"))

type BlameOutput struct {
	CommitMap map[string]blame.Commit
	HunkMap   map[string][]blame.Hunk
}

var blameIgnores = []string{
	"node_modules", "bower_components",
	"doc", "docs", "build", "vendor",
	".min.js", "-min.js", ".optimized.js", "-optimized.js",
	"dist", "assets", "deps/", "dep/", ".jar", ".png", ".html",
	"third-party",
}

func BlameFiles(dir string, files []string, commitID string, c *config.Repository, x *task2.Context) (*BlameOutput, error) {
	if SkipBlame {
		x.Log.Printf("Skipping VCS blame (returning empty BlameOutput)")
		return new(BlameOutput), nil
	}

	hunkMap := make(map[string][]blame.Hunk)
	commitMap := make(map[string]blame.Commit)

	for _, file := range files {
		relFile, err := filepath.Rel(dir, file)
		if err != nil {
			return nil, err
		}

		hunks, commitMap2, err := blame.BlameFile(dir, relFile, commitID)
		if err != nil {
			return nil, err
		}
		hunkMap[relFile] = hunks
		for cid, cm := range commitMap2 {
			if _, present := commitMap[cid]; !present {
				commitMap[cid] = cm
			}
		}
	}

	return &BlameOutput{commitMap, hunkMap}, nil
}
