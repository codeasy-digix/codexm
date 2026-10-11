#!/usr/bin/env node
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const {spawn} = require('node:child_process');
const architecture = process.arch === 'x64' ? 'amd64' : process.arch;
const platform = `${process.platform}-${architecture}`;
if (!['darwin-arm64', 'darwin-amd64', 'linux-arm64', 'linux-amd64'].includes(platform)) {
  console.error(`codexm: unsupported platform ${platform}`);
  process.exit(2);
}
const binary = path.join(__dirname, '..', 'bin', platform, 'codexm');
if (!fs.existsSync(binary)) {
  console.error('codexm: prepared native package is incomplete');
  process.exit(1);
}
const child = spawn(binary, process.argv.slice(2), {stdio: 'inherit', env: process.env});
// Terminal Ctrl+C reaches the foreground child directly. Keep the launcher
// alive so the official viewer can interrupt a turn without losing its window.
process.on('SIGINT', () => {});
for (const signal of ['SIGTERM', 'SIGHUP']) {
  process.on(signal, () => { if (!child.killed) child.kill(signal); });
}
child.on('error', (error) => {
  console.error(`codexm: ${error.message}`);
  process.exit(1);
});
child.on('close', (code, signal) => {
  if (signal) {
    process.removeAllListeners(signal);
    process.kill(process.pid, signal);
  } else process.exit(code === null ? 1 : code);
});
