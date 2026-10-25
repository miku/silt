package silt

import (
	"runtime/debug"
	"strings"
)

const modulePath = "github.com/miku/silt"

// version is set at build time:
//
//	go build -ldflags "-X github.com/miku/silt.version=0.1.0" ./cmd/silt
var version string

// Version reports silt's version: the one stamped in with -ldflags if any,
// else the module version recorded by the go command (as for go install
// github.com/miku/silt/cmd/silt@v0.1.0), else "0.0.0-dev".
func Version() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		mods := append([]*debug.Module{&bi.Main}, bi.Deps...)
		for _, m := range mods {
			if m.Path == modulePath && m.Version != "" && m.Version != "(devel)" {
				return strings.TrimPrefix(m.Version, "v")
			}
		}
	}
	return "0.0.0-dev"
}
