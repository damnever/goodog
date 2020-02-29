package goodog

import (
	"strings"
)

var (
	version   = "0.1.0"
	buildDate = ""
	gitSHA    = ""
)

func Version() string {
	return version
}

func VersionInfo() string {
	info := &strings.Builder{}
	info.WriteString("goodog ")
	info.WriteString(version)
	if buildDate != "" || gitSHA != "" {
		info.WriteString(" (")
		space := false
		if gitSHA != "" {
			space = true
			info.WriteString("git@")
			info.WriteString(gitSHA)
		}
		if buildDate != "" {
			if space {
				info.WriteString(" ")
			}
			info.WriteString("date@")
			info.WriteString(buildDate)
		}
		info.WriteString(")")
	}
	return info.String()
}
