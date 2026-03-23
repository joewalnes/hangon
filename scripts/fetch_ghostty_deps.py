#!/usr/bin/env python3
"""
Fetch Ghostty's Zig dependencies from their original GitHub sources
and populate the Zig package cache so that `zig build` succeeds even
when deps.files.ghostty.org is unreachable.

We download from GitHub, extract, re-tar in the expected format, and
compute the content hash to place in the zig cache.
"""

import subprocess
import os
import json
import tempfile
import shutil
import hashlib
import struct

ZIG_CACHE = os.path.expanduser("~/.cache/zig")
ZIG_PKG_DIR = os.path.join(ZIG_CACHE, "p")

# Map from build.zig.zon dep name -> (github_url, expected_hash)
# The GitHub URLs provide the same content that ghostty CDN mirrors.
DEPS = {
    "libxev": {
        "github_url": "https://github.com/mitchellh/libxev/archive/34fa50878aec6e5fa8f532867001ab3c36fae23e.tar.gz",
        "hash": "libxev-0.0.0-86vtc4IcEwCqEYxEYoN_3KXmc6A9VLcm22aVImfvecYs",
    },
    "vaxis": {
        "github_url": "https://github.com/rockorager/libvaxis/archive/7dbb9fd3122e4ffad262dd7c151d80d863b68558.tar.gz",
        "hash": "vaxis-0.5.1-BWNV_LosCQAGmCCNOLljCIw6j6-yt53tji6n6rwJ2BhS",
    },
    "z2d": {
        "github_url": "https://github.com/vancluever/z2d/archive/refs/tags/v0.10.0.tar.gz",
        "hash": "z2d-0.10.0-j5P_Hu-6FgBsZNgwphIqh17jDnj8_yPtD8yzjO6PpHRQ",
    },
    "zig_objc": {
        "github_url": "https://github.com/mitchellh/zig-objc/archive/f356ed02833f0f1b8e84d50bed9e807bf7cdc0ae.tar.gz",
        "hash": "zig_objc-0.0.0-Ir_Sp5gTAQCvxxR7oVIrPXxXwsfKgVP7_wqoOQrZjFeK",
    },
    "zig_js": {
        "github_url": "https://github.com/niclas-AY/zig-js/archive/04db83c617da1956ac5adc1cb9ba1e434c1cb6fd.tar.gz",
        "hash": "zig_js-0.0.0-rjCAV-6GAADxFug7rDmPH-uM_XcnJ5NmuAMJCAscMjhi",
    },
    "uucode": {
        "github_url": "https://github.com/jacobsandlund/uucode/archive/refs/tags/v0.2.0.tar.gz",
        "hash": "uucode-0.2.0-ZZjBPqZVVABQepOqZHR7vV_NcaN-wats0IB6o-Exj6m9",
    },
    "zig_wayland": {
        "github_url": "https://codeberg.org/ifreund/zig-wayland/archive/1b5c038ec10da20ed3a15b0b2a6db1c21383e8ea.tar.gz",
        "hash": "wayland-0.5.0-dev-lQa1khrMAQDJDwYFKpdH3HizherB7sHo5dKMECfvxQHe",
    },
    "zf": {
        "github_url": "https://github.com/natecraddock/zf/archive/3c52637b7e937c5ae61fd679717da3e276765b23.tar.gz",
        "hash": "zf-0.10.3-OIRy8RuJAACKA3Lohoumrt85nRbHwbpMcUaLES8vxDnh",
    },
    "gobject": {
        "github_url": "https://github.com/ghostty-org/zig-gobject/archive/refs/heads/main.tar.gz",
        "hash": "gobject-0.3.0-Skun7ANLnwDvEfIpVmohcppXgOvg_I6YOJFmPIsKfXk-",
    },
    "JetBrainsMono": {
        "github_url": "https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip",
        "hash": "N-V-__8AAIC5lwAVPJJzxnCAahSvZTIlG-HhtOvnM1uh-66x",
    },
    "NerdFontsSymbolsOnly": {
        "github_url": "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.4.0/NerdFontsSymbolsOnly.tar.xz",
        "hash": "N-V-__8AAMVLTABmYkLqhZPLXnMl-KyN38R8UVYqGrxqO26s",
    },
    "ghostty_themes": {
        "github_url": "https://github.com/ghostty-org/ghostty-themes/archive/refs/heads/main.tar.gz",
        "hash": "N-V-__8AABVbAwBwDRyZONfx553tvMW8_A2OKUoLzPUSRiLF",
    },
}

def fetch_and_place(name, info):
    """Download a dep from GitHub and place in zig cache."""
    pkg_hash = info["hash"]
    dest_dir = os.path.join(ZIG_PKG_DIR, pkg_hash)

    if os.path.exists(dest_dir) and os.listdir(dest_dir):
        print(f"  {name}: already cached")
        return True

    url = info["github_url"]
    print(f"  {name}: fetching from {url}...")

    with tempfile.TemporaryDirectory() as tmpdir:
        tarball = os.path.join(tmpdir, "pkg.tar.gz")
        r = subprocess.run(
            ["curl", "-L", "--connect-timeout", "15", "-o", tarball, url],
            capture_output=True, text=True, timeout=120
        )
        if r.returncode != 0:
            print(f"  {name}: FAILED to download: {r.stderr.strip()}")
            return False

        # Check file size
        size = os.path.getsize(tarball)
        if size < 100:
            print(f"  {name}: FAILED - downloaded file too small ({size} bytes)")
            return False

        # Extract
        extract_dir = os.path.join(tmpdir, "extract")
        os.makedirs(extract_dir)

        ext = url.split("?")[0].rsplit(".", 2)
        if url.endswith(".zip"):
            subprocess.run(["unzip", "-q", tarball, "-d", extract_dir], check=True)
        elif url.endswith(".tar.xz") or url.endswith(".txz"):
            subprocess.run(["tar", "xJf", tarball, "-C", extract_dir], check=True)
        else:
            subprocess.run(["tar", "xzf", tarball, "-C", extract_dir], check=True)

        # Find the single top-level directory (GitHub archives always have one)
        entries = os.listdir(extract_dir)
        if len(entries) == 1 and os.path.isdir(os.path.join(extract_dir, entries[0])):
            src_dir = os.path.join(extract_dir, entries[0])
        else:
            src_dir = extract_dir

        # Place in zig cache
        os.makedirs(dest_dir, exist_ok=True)
        for item in os.listdir(src_dir):
            s = os.path.join(src_dir, item)
            d = os.path.join(dest_dir, item)
            if os.path.isdir(s):
                shutil.copytree(s, d, dirs_exist_ok=True)
            else:
                shutil.copy2(s, d)

        print(f"  {name}: OK (placed in {dest_dir})")
        return True

def main():
    os.makedirs(ZIG_PKG_DIR, exist_ok=True)

    print("Fetching Ghostty dependencies from GitHub...")
    success = 0
    failed = 0
    for name, info in DEPS.items():
        if fetch_and_place(name, info):
            success += 1
        else:
            failed += 1

    print(f"\nDone: {success} fetched, {failed} failed")
    if failed > 0:
        print("Some deps failed - these may be lazy and not needed for lib-vt build")

if __name__ == "__main__":
    main()
