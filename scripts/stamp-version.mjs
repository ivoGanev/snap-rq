// Stamps a release version into every file that carries the app version.
// Node is guaranteed present in CI (setup-node) and behaves identically
// across Windows/macOS/Linux, unlike sed -i which differs per platform.
//
// Usage: node scripts/stamp-version.mjs <version>   e.g. node scripts/stamp-version.mjs 1.2.3

import { readFileSync, writeFileSync } from 'node:fs';

const version = process.argv[2];
if (!version) {
  console.error('usage: stamp-version.mjs <version>');
  process.exit(1);
}
if (!/^\d+\.\d+\.\d+$/.test(version)) {
  // Windows version resources need exactly three numeric parts.
  console.error(`invalid version "${version}": expected X.Y.Z`);
  process.exit(1);
}

const targets = [
  // build/config.yml: `version: "0.0.1"`
  { path: 'build/config.yml', re: /^(  version: ).*$/m, repl: `$1"${version}"` },
  // build/windows/info.json: `"file_version": "0.1.0"` and `"ProductVersion": "0.1.0"`
  { path: 'build/windows/info.json', re: /("(?:file_version|ProductVersion)": ")[^"]*(")/g, repl: `$1${version}$2` },
  // build/linux/nfpm/nfpm.yaml: `version: "0.1.0"`
  { path: 'build/linux/nfpm/nfpm.yaml', re: /^(version: ).*$/m, repl: `$1"${version}"` },
  // build/windows/nsis/wails_tools.nsh: `!define INFO_PRODUCTVERSION "0.1.0"` (installer metadata)
  { path: 'build/windows/nsis/wails_tools.nsh', re: /(!define INFO_PRODUCTVERSION ")[^"]*(")/, repl: `$1${version}$2` },
  // build/darwin/Info.plist: CFBundleVersion and CFBundleShortVersionString
  { path: 'build/darwin/Info.plist', re: /(<key>CFBundleVersion<\/key>\s*<string>)[^<]*(<\/string>)/g, repl: `$1${version}$2` },
  { path: 'build/darwin/Info.plist', re: /(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]*(<\/string>)/g, repl: `$1${version}$2` },
];

for (const { path, re, repl } of targets) {
  try {
    const content = readFileSync(path, 'utf8');
    writeFileSync(path, content.replace(re, repl));
  } catch (err) {
    if (err.code === 'ENOENT') {
      console.warn(`warning: ${path} not found, skipping`);
      continue;
    }
    throw err;
  }
}

console.log(`version stamped to ${version}`);
