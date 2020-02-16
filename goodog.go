package goodog

import (
	"runtime/debug"
	"strings"
)

var (
	BuildDate = "unknown"
	GitRev    = "unknown"
)

const ImportPath = "github.com/damnever/goodog"

func Version() string {
	mod := goModule()
	return mod.Version
}

func VersionInfo() string {
	mod := goModule()
	info := &strings.Builder{}
	info.WriteString("goodog ")
	info.WriteString(mod.Version)
	if mod.Sum != "" || BuildDate != "unknown" || GitRev != "unknown" {
		info.WriteString(" [")
		space := false
		if mod.Sum != "" {
			space = true
			info.WriteString("sum@")
			info.WriteString(mod.Sum)
		}
		if BuildDate != "unknown" {
			if space {
				info.WriteString(" ")
			}
			space = true
			info.WriteString("date@")
			info.WriteString(BuildDate)
		}
		if GitRev != "unknown" {
			if space {
				info.WriteString(" ")
			}
			info.WriteString("git@")
			info.WriteString(GitRev)
		}
		info.WriteString("]")
	}
	return info.String()
}

// Modified from https://github.com/caddyserver/caddy/blob/v2/caddy.go#L520-L540
func goModule() debug.Module {
	defltmod := debug.Module{Version: "unknown"}
	bi, ok := debug.ReadBuildInfo()
	if ok {
		defltmod.Path = bi.Main.Path
		// TODO: track related Go issue: https://github.com/golang/go/issues/29228
		// once that issue is fixed, we should just be able to use bi.Main... hopefully.
		for _, dep := range bi.Deps {
			if dep.Path == ImportPath {
				return *dep
			}
		}
		return bi.Main
	}
	return defltmod
}
