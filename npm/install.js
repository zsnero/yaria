#!/usr/bin/env node

const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");
const { createGunzip } = require("zlib");
const { pipeline } = require("stream");

const BASE_URL = "https://yaria.live/releases";
const BIN_DIR = path.join(__dirname, "bin");
const VERSION = require("./package.json").version;

function getPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const platforms = {
    "linux-x64": "yaria-linux-amd64",
    "linux-arm64": "yaria-linux-arm64",
    "darwin-x64": "yaria-darwin-amd64",
    "darwin-arm64": "yaria-darwin-arm64",
    "win32-x64": "yaria-windows-amd64.exe",
  };

  const key = `${platform}-${arch}`;
  const binary = platforms[key];

  if (!binary) {
    console.error(`Unsupported platform: ${platform}-${arch}`);
    console.error(`Supported: ${Object.keys(platforms).join(", ")}`);
    process.exit(1);
  }

  return { binary, isWindows: platform === "win32" };
}

function download(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith("https") ? https : http;
    client
      .get(url, (res) => {
        // Handle redirects
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return download(res.headers.location).then(resolve).catch(reject);
        }
        if (res.statusCode !== 200) {
          reject(new Error(`HTTP ${res.statusCode}: ${url}`));
          return;
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

async function main() {
  const { binary, isWindows } = getPlatform();
  const url = `${BASE_URL}/${binary}`;
  const binName = isWindows ? "yaria.exe" : "yaria";
  const binPath = path.join(BIN_DIR, binName);

  // Create bin directory
  fs.mkdirSync(BIN_DIR, { recursive: true });

  console.log(`Downloading Yaria v${VERSION} for ${process.platform}-${process.arch}...`);
  console.log(`  ${url}`);

  try {
    const data = await download(url);
    fs.writeFileSync(binPath, data);

    if (!isWindows) {
      fs.chmodSync(binPath, 0o755);
    }

    console.log(`Installed to ${binPath}`);
  } catch (err) {
    console.error(`Failed to download Yaria: ${err.message}`);
    console.error("");
    console.error("You can install manually:");
    console.error("  https://github.com/zsnero/yaria/releases");
    process.exit(1);
  }
}

main();
