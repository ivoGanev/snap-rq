// Package identity holds the names installed copies of the app depend on to
// find their data, their secrets key and their updates. Changing one orphans
// existing installs unless a migration moves them along; identity_test.go
// pins every value so that only a deliberate change gets through.
package identity

const (
	// DataDirName is the data folder under the OS data directory.
	DataDirName = "SnapRQ"
	// DatabaseFile is the database inside the data folder.
	DatabaseFile = "app.db"

	// KeyringService and KeyringAccount find the secrets key in the OS
	// credential store (Keychain, Credential Manager, Secret Service).
	KeyringService = "SnapRQ"
	KeyringAccount = "variable-encryption-key"

	// BundleID identifies the app to macOS (Info.plist) and Windows (MSIX).
	BundleID = "com.iganev.snaprq"

	// UpdaterRepository (owner/repo) publishes the releases installed apps update from.
	UpdaterRepository = "ivoGanev/snap-rq"
	// ChecksumAsset is the release file listing every asset's SHA-256.
	ChecksumAsset = "SHA256SUMS"
)

// PreviousDataDirNames are the data folder's earlier names. On start, the
// first one found is moved to DataDirName when that doesn't exist yet.
var PreviousDataDirNames = []string{}
