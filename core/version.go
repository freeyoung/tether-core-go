package core

import (
	"strconv"
	"strings"
)

// The apps set these with -ldflags -X: Version is the version of the app, and
// MinPeerVersion the oldest version of a peer that it syncs with.
var (
	Version        = "0.0.0-dev"
	MinPeerVersion = "0.0.0"
)

// versionFeature names the app and its version in hello. Every app with this
// engine sends it under the name of the first one, so that they know each
// other. tetherd and the iPhone app ignore features they do not know.
const versionFeature = "tether-desktop/"

// peerTooOld tells whether a peer's version is below MinPeerVersion. The
// minimum 0.0.0 means none, so that builds without a release version
// (0.0.0-dev) still work with each other.
func peerTooOld(version string) bool {
	return CompareVersions(MinPeerVersion, "0.0.0") > 0 && CompareVersions(version, MinPeerVersion) < 0
}

func helloFeatures() []string {
	return append(append([]string{}, features...), versionFeature+Version)
}

// peerVersion returns the version in the hello of a peer with this engine, or
// false for a peer that names none (tetherd, the iPhone app).
func peerVersion(features []string) (string, bool) {
	for _, f := range features {
		if v, ok := strings.CutPrefix(f, versionFeature); ok {
			return v, true
		}
	}
	return "", false
}

// CompareVersions compares versions such as "0.3.0", "v0.3.1" and
// "0.4.0-rc.1": -1, 0 or 1. A version with a suffix comes before the same
// version without one. Parts that are not numbers count as 0.
func CompareVersions(a, b string) int {
	na, pa := splitVersion(a)
	nb, pb := splitVersion(b)
	for i := range 3 {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	case pa < pb:
		return -1
	}
	return 1
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		n[i], _ = strconv.Atoi(part)
	}
	return n, pre
}
