package goodog

import (
	"strings"
)

var (
	_version   = "0.1.0"
	_buildDate = ""
	_gitSHA    = ""
)

func Version() string {
	return _version
}

func VersionInfo() string {
	info := &strings.Builder{}
	info.WriteString("goodog ")
	info.WriteString(_version)
	if _buildDate != "" || _gitSHA != "" {
		info.WriteString(" (")
		space := false
		if _gitSHA != "" {
			space = true
			info.WriteString("git@")
			info.WriteString(_gitSHA)
		}
		if _buildDate != "" {
			if space {
				info.WriteString(" ")
			}
			info.WriteString("date@")
			info.WriteString(_buildDate)
		}
		info.WriteString(")")
	}
	return info.String()
}
