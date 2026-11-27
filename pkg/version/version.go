// Package version identifies which build of envset is running, so builds on
// different Macs can be told apart.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is set at build time by the Makefile and scripts/build-branches.sh with
// -ldflags "-X github.com/experimentalist/envset/pkg/version.Version=<branch>@<commit>".
var Version = ""

// String is the build's version: the value set at build time, else the module
// version (go install …@v1.2.3), else the git commit Go recorded, else "devel".
func String() string {
	info, _ := debug.ReadBuildInfo()
	return resolve(Version, info)
}

func resolve(set string, info *debug.BuildInfo) string {
	if set != "" {
		return set
	}
	if info == nil {
		return "devel"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "+modified"
			}
		}
	}
	if rev == "" {
		return "devel"
	}
	return "devel-" + rev[:min(len(rev), 7)] + dirty
}

// Full is the line printed by "envset version".
func Full() string {
	return fmt.Sprintf("envset %s (%s, %s/%s)", String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
