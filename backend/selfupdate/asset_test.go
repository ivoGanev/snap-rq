package selfupdate

import "testing"

func TestIsUpdateAsset(t *testing.T) {
	assets := []string{
		"snaprq-windows-amd64.exe",
		"snaprq-windows-amd64-setup.exe",
		"snaprq-darwin-universal.zip",
		"snaprq-darwin-universal.dmg",
		"snaprq-linux-amd64",
		"snaprq-linux-amd64.AppImage",
		"snaprq-linux-amd64.deb",
		"snaprq-linux-amd64.rpm",
		"SHA256SUMS",
	}
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"windows", "amd64", "snaprq-windows-amd64.exe"},
		{"darwin", "arm64", "snaprq-darwin-universal.zip"},
		{"darwin", "amd64", "snaprq-darwin-universal.zip"},
		{"linux", "amd64", "snaprq-linux-amd64"},
		{"linux", "arm64", ""},
		{"windows", "arm64", ""},
		{"freebsd", "amd64", ""},
	}
	for _, c := range cases {
		got := ""
		count := 0
		for _, a := range assets {
			if IsUpdateAsset(a, c.goos, c.goarch) {
				got = a
				count++
			}
		}
		if got != c.want || count > 1 {
			t.Errorf("%s/%s: got %q (%d matches), want %q", c.goos, c.goarch, got, count, c.want)
		}
	}
}
