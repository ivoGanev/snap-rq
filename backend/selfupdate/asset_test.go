package selfupdate

import "testing"

func TestIsUpdateAsset(t *testing.T) {
	assets := []string{
		"snap-rq-wails-v3-windows-amd64.exe",
		"snap-rq-wails-v3-windows-amd64-setup.exe",
		"snap-rq-wails-v3-darwin-universal.zip",
		"snap-rq-wails-v3-darwin-universal.dmg",
		"snap-rq-wails-v3-linux-amd64",
		"snap-rq-wails-v3-linux-amd64.AppImage",
		"snap-rq-wails-v3-linux-amd64.deb",
		"snap-rq-wails-v3-linux-amd64.rpm",
		"SHA256SUMS",
	}
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"windows", "amd64", "snap-rq-wails-v3-windows-amd64.exe"},
		{"darwin", "arm64", "snap-rq-wails-v3-darwin-universal.zip"},
		{"darwin", "amd64", "snap-rq-wails-v3-darwin-universal.zip"},
		{"linux", "amd64", "snap-rq-wails-v3-linux-amd64"},
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
