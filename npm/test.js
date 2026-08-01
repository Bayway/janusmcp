'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

const launcherPath = path.join(__dirname, 'bin', 'janusmcp.js');
const launcherSource = fs.readFileSync(launcherPath, 'utf8');

test('launcher uses the JanusMCP binary and package names', () => {
  assert.match(launcherSource, /isWin \? 'janusmcp\.exe' : 'janusmcp'/);
  assert.match(launcherSource, /npm i -g @bayway\/janusmcp/);
  assert.doesNotMatch(launcherSource, /multimcp/i);
});

test('launcher forwards arguments and native exit status', { skip: process.platform === 'win32' }, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'janusmcp-launcher-'));
  try {
    const launcher = path.join(dir, 'janusmcp.js');
    const binary = path.join(dir, 'janusmcp');
    fs.copyFileSync(launcherPath, launcher);
    fs.writeFileSync(
      binary,
      '#!/usr/bin/env node\nconsole.log(JSON.stringify(process.argv.slice(2)));\nprocess.exit(7);\n',
      { mode: 0o755 },
    );

    const result = spawnSync(
      process.execPath,
      [launcher, 'call', 'ping', '--json'],
      { encoding: 'utf8' },
    );

    assert.equal(result.status, 7);
    assert.deepEqual(JSON.parse(result.stdout), ['call', 'ping', '--json']);
    assert.equal(result.stderr, '');
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
