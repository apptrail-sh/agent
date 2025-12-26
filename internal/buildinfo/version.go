package buildinfo

import "runtime/debug"

// AgentVersion returns the build version or revision for the running binary.
func AgentVersion() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return "dev"
}
