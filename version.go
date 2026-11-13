package main

import (
	"runtime/debug"
	"strings"
)

// version is set at release time: -ldflags "-X main.version=v1.2.3".
var version = ""

// Version is the release tag, the module version for go install builds, or "dev"
// with the commit for local builds.
func Version() string {
	if version != "" {
		return "v" + strings.TrimPrefix(version, "v")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value[:min(7, len(s.Value))]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	switch {
	case rev == "":
		return "dev"
	case dirty:
		return "dev-" + rev + "-dirty"
	default:
		return "dev-" + rev
	}
}
