package version

import (
	"runtime/debug"
	"strings"
)

var (
	Version  = "devel"
	Commit   = "unknown"
	BuiltAt  = "unknown"
	Modified bool
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = strings.TrimPrefix(info.Main.Version, "v")
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if Commit == "unknown" && setting.Value != "" {
				Commit = setting.Value
			}
		case "vcs.time":
			if BuiltAt == "unknown" && setting.Value != "" {
				BuiltAt = setting.Value
			}
		case "vcs.modified":
			Modified = setting.Value == "true"
		}
	}
}

// Identifier binds an application version to the exact source revision when
// Go's VCS build metadata is available.
func Identifier() string {
	identifier := Version
	if Commit != "unknown" {
		revision := Commit
		if len(revision) > 12 {
			revision = revision[:12]
		}
		identifier += "+" + revision
	}
	if Modified {
		identifier += ".modified"
	}
	return identifier
}
