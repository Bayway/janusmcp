#!/usr/bin/env node
// Launcher: forwards args to the downloaded native binary, passing stdio through
// untouched (required for the MCP stdio transport).
'use strict';

const path = require('path');
const fs = require('fs');
const { spawnSync } = require('child_process');

const isWin = process.platform === 'win32';
const bin = path.join(__dirname, isWin ? 'multimcp.exe' : 'multimcp');

if (!fs.existsSync(bin)) {
  console.error('[multimcp] native binary missing — reinstall the package (npm i -g multimcp).');
  process.exit(1);
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
if (res.error) {
  console.error('[multimcp] ' + res.error.message);
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
