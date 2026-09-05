import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/set-version.py'

class ReleaseVersionTests(unittest.TestCase):
    def test_updates_both_versions_without_changing_dependencies(self):
        for version in ['1.2.3', '1.2.3-rc.1']:
            with self.subTest(version=version), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp) / 'src-tauri'
                root.mkdir()
                cargo = root / 'Cargo.toml'
                cargo.write_text('[package]\nversion = "0.1.0"\n[dependencies]\nserde = { version = "1" }\n')
                config = root / 'tauri.conf.json'
                config.write_text('{"version":"0.1.0","productName":"dock-util"}')
                subprocess.run([sys.executable, str(SCRIPT), version], cwd=tmp, check=True)
                self.assertEqual(cargo.read_text(), f'[package]\nversion = "{version}"\n[dependencies]\nserde = {{ version = "1" }}\n')
                self.assertEqual(json.loads(config.read_text()), {'version':version,'productName':'dock-util'})

if __name__ == '__main__':
    unittest.main()
