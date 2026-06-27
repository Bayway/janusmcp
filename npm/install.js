// Postinstall: download the prebuilt `janusmcp` binary that matches this package
// version from the GitHub release, and place it next to the launcher.
//
// Asset naming must match .goreleaser.yaml archives:
//   janusmcp_<os>_<arch>.tar.gz   (zip on windows)
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const https = require('https');
const { execFileSync } = require('child_process');

const REPO = 'bayway/janusmcp';
const version = require('./package.json').version;

const OS_MAP = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
const ARCH_MAP = { x64: 'amd64', arm64: 'arm64' };

function fail(msg) {
  console.error('[janusmcp] ' + msg);
  console.error('[janusmcp] You can also install from https://github.com/' + REPO + '/releases');
  process.exit(1);
}

const goos = OS_MAP[process.platform];
const goarch = ARCH_MAP[process.arch];
if (!goos || !goarch) fail(`unsupported platform ${process.platform}/${process.arch}`);

const isWin = goos === 'windows';
const ext = isWin ? 'zip' : 'tar.gz';
const asset = `janusmcp_${goos}_${goarch}.${ext}`;
const url = `https://github.com/${REPO}/releases/download/v${version}/${asset}`;

const binDir = path.join(__dirname, 'bin');
fs.mkdirSync(binDir, { recursive: true });
const tmp = path.join(os.tmpdir(), `janusmcp-${version}-${Date.now()}.${ext}`);

function download(u, dest, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 10) return reject(new Error('too many redirects'));
    https.get(u, { headers: { 'User-Agent': 'janusmcp-npm-installer' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        return resolve(download(res.headers.location, dest, redirects + 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`HTTP ${res.statusCode} for ${u}`));
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on('finish', () => file.close(resolve));
      file.on('error', reject);
    }).on('error', reject);
  });
}

(async () => {
  try {
    console.error(`[janusmcp] downloading ${asset} (v${version})…`);
    await download(url, tmp);

    // bsdtar (present on macOS, Linux, and Windows 10+) extracts both tar.gz and zip.
    execFileSync('tar', ['-xf', tmp, '-C', binDir], { stdio: 'inherit' });
    fs.rmSync(tmp, { force: true });

    const binPath = path.join(binDir, isWin ? 'janusmcp.exe' : 'janusmcp');
    if (!fs.existsSync(binPath)) fail('binary not found in archive after extraction');
    if (!isWin) fs.chmodSync(binPath, 0o755);
    console.error('[janusmcp] installed.');
  } catch (e) {
    fail('install failed: ' + e.message);
  }
})();
