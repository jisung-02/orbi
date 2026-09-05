#!/usr/bin/env python3
"""Set the release version in Cargo and Tauri metadata."""
import json
import re
import sys
from pathlib import Path

version = sys.argv[1]
if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", version):
    sys.exit("Invalid release version")
manifest = Path("src-tauri/Cargo.toml")
text, count = re.subn(r'^version = "[^"]+"$', f'version = "{version}"',
                      manifest.read_text(), count=1, flags=re.MULTILINE)
if count != 1:
    sys.exit("Package version not found")
config_path = Path("src-tauri/tauri.conf.json")
config = json.loads(config_path.read_text())
config["version"] = version
manifest.write_text(text)
config_path.write_text(json.dumps(config, indent=2, ensure_ascii=False) + "\n")
