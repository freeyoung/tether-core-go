package core

import (
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

// modulePath is this module, as the build information of an app names it.
const modulePath = "github.com/freeyoung/tether-core-go"

const devVersion = "0.0.0-dev"

// EngineVersion is the version of this module in the build of the app, such
// as "0.5.0", or 0.0.0-dev for a build from a local copy. The engine names it
// in hello, so that peers can refuse an engine that is too old. The version
// of the app is the app's own business.
var EngineVersion = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion
	}
	return engineVersionOf(bi)
})

// engineVersionOf finds this module in the build information: as a
// dependency of an app, or as the main module of its tools and tests.
func engineVersionOf(bi *debug.BuildInfo) string {
	m := &bi.Main
	for _, d := range bi.Deps {
		if d.Path == modulePath {
			m = d
			break
		}
	}
	if m.Path != modulePath {
		return devVersion
	}
	// A replace with a local copy has no version.
	if m.Replace != nil {
		m = m.Replace
	}
	v := strings.TrimPrefix(m.Version, "v")
	if v == "" || v == "(devel)" {
		return devVersion
	}
	return v
}

// MinPeerVersion is the oldest engine version that this engine syncs with.
// A release of the engine raises it when older engines must no longer sync,
// for example after a security fix. 0.0.0 means none.
var MinPeerVersion = "0.0.0"

// versionFeature names the engine and its version in hello. Every app with
// this engine sends it, under the name that the first app gave it, so that
// older peers still know it. tetherd and the iPhone app ignore features they
// do not know.
const versionFeature = "tether-desktop/"

// peerTooOld tells whether a peer's engine is below MinPeerVersion. The
// minimum 0.0.0 means none, so that builds without a release version
// (0.0.0-dev) still work with each other.
func peerTooOld(version string) bool {
	return CompareVersions(MinPeerVersion, "0.0.0") > 0 && CompareVersions(version, MinPeerVersion) < 0
}

func helloFeatures() []string {
	return append(append([]string{}, features...), versionFeature+EngineVersion())
}

// peerVersion returns the engine version in the hello of a peer with this
// engine, or false for a peer that names none (tetherd, the iPhone app).
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
