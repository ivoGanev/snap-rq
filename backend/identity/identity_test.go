package identity

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// Installed apps look for these exact values. A failure here means a release
// would lose users' data, secrets or updates: change a value only together
// with a migration, then update this test.
const orphanWarning = "changing this orphans existing installs; add a migration and update identity_test.go deliberately"

func TestValuesInstalledAppsDependOn(t *testing.T) {
	pinned := map[string][2]string{
		"DataDirName":       {DataDirName, "SnapRQ"},
		"DatabaseFile":      {DatabaseFile, "app.db"},
		"KeyringService":    {KeyringService, "SnapRQ"},
		"KeyringAccount":    {KeyringAccount, "variable-encryption-key"},
		"BundleID":          {BundleID, "com.iganev.snaprq"},
		"UpdaterRepository": {UpdaterRepository, "ivoGanev/snap-rq"},
		"ChecksumAsset":     {ChecksumAsset, "SHA256SUMS"},
	}
	for name, v := range pinned {
		if v[0] != v[1] {
			t.Errorf("%s is %q, released as %q: %s", name, v[0], v[1], orphanWarning)
		}
	}
}

// Every name the data folder has had in a release, oldest first. Renaming the
// folder means appending the new name here and adding the old one to
// PreviousDataDirNames, so the app moves existing profiles on start.
var releasedDataDirNames = []string{"SnapRQ"}

func TestDataFolderRenamesAreMigrated(t *testing.T) {
	current := releasedDataDirNames[len(releasedDataDirNames)-1]
	if DataDirName != current {
		t.Fatalf("DataDirName is %q, but the last released name is %q: %s", DataDirName, current, orphanWarning)
	}
	for _, old := range releasedDataDirNames[:len(releasedDataDirNames)-1] {
		if !slices.Contains(PreviousDataDirNames, old) {
			t.Errorf("released data folder %q isn't in PreviousDataDirNames, so its users would start with no data", old)
		}
	}
}

// The bundle ID also lives in the build files that package the app.
func TestBuildFilesUseTheBundleID(t *testing.T) {
	build := filepath.Join("..", "..", "build")
	files := map[string]*regexp.Regexp{
		"config.yml":                    regexp.MustCompile(`productIdentifier:\s*"([^"]+)"`),
		"darwin/Info.plist":             regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>([^<]+)</string>`),
		"darwin/Info.dev.plist":         regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>([^<]+)</string>`),
		"windows/msix/app_manifest.xml": regexp.MustCompile(`<Identity\s+Name="([^"]+)"`),
		"windows/msix/template.xml":     regexp.MustCompile(`<Application\s+Id="([^"]+)"`),
	}
	for file, pattern := range files {
		raw, err := os.ReadFile(filepath.Join(build, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		match := pattern.FindSubmatch(raw)
		if match == nil {
			t.Errorf("build/%s: no bundle ID found", file)
			continue
		}
		if got := string(match[1]); got != BundleID {
			t.Errorf("build/%s has bundle ID %q, want %q: %s", file, got, BundleID, orphanWarning)
		}
	}
}
