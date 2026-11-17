import tempfile
from pathlib import Path
import unittest

class FrontendSnapshotTests(unittest.TestCase):
    def test_snapshot_excludes_credentials_and_paused_sources(self):
        from capacity_frontend import snapshot_frontend
        with tempfile.TemporaryDirectory() as a, tempfile.TemporaryDirectory() as b:
            source=Path(a); target=Path(b)/'frontend'
            for name in ('index.html','package.json','package-lock.json','tsconfig.json','vite.config.ts','nginx.conf'):
                (source/name).write_text('synthetic source')
            (source/'src').mkdir();(source/'src'/'app.ts').write_text('app')
            (source/'public').mkdir();(source/'public'/'logo.svg').write_text('svg')
            (source/'.env').write_text('private');(source/'docs').mkdir();(source/'docs'/'private.md').write_text('excluded')
            digest=snapshot_frontend(source,target)
            self.assertEqual(len(digest),64)
            self.assertTrue((target/'src'/'app.ts').exists())
            self.assertFalse((target/'.env').exists());self.assertFalse((target/'docs').exists())

if __name__=='__main__': unittest.main()
