package version

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	vcs := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}
	vcs.Main.Version = "(devel)"
	tagged := &debug.BuildInfo{}
	tagged.Main.Version = "v0.1.0"
	cases := []struct {
		set  string
		info *debug.BuildInfo
		want string
	}{
		{"v0.1.0-3-gabc1234", tagged, "v0.1.0-3-gabc1234"},
		{"", tagged, "v0.1.0"},
		{"", vcs, "devel-0123456+modified"},
		{"", &debug.BuildInfo{}, "devel"},
		{"", nil, "devel"},
	}
	for _, c := range cases {
		if got := resolve(c.set, c.info); got != c.want {
			t.Errorf("resolve(%q) = %q, want %q", c.set, got, c.want)
		}
	}
}
