"""Tests the smoke test's check that /play/ loads the uploaded web client (web_client_page.py)."""

import unittest

from web_client_page import web_bundle_referenced

# A real build's dist/index.html, from `bun run build` in clients/island-battles-web,
# with its hashed file names. The shape matters: the module tag carries the
# script's path, quoted, under /play/.
BUILT_INDEX_HTML = """<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Island Battles</title>
    <link rel="icon" href="data:," />


  <link rel="stylesheet" crossorigin href="/play/assets/index-jgw597s7.css"><script type="module" crossorigin src="/play/assets/index-rt74q59x.js"></script></head>
  <body>
    <canvas id="scene" aria-label="Island Battles battlefield"></canvas>
  </body>
</html>
"""

SCRIPT = "assets/index-rt74q59x.js"


class WebBundleReferencedTest(unittest.TestCase):
    def test_built_page_loads_its_entry_script(self):
        self.assertTrue(web_bundle_referenced(BUILT_INDEX_HTML, SCRIPT))

    def test_other_build_is_not_referenced(self):
        self.assertFalse(web_bundle_referenced(BUILT_INDEX_HTML, "assets/index-zzzzzzzz.js"))

    def test_script_without_the_play_prefix_is_not_referenced(self):
        page = BUILT_INDEX_HTML.replace('"/play/assets/', '"/assets/')
        self.assertFalse(web_bundle_referenced(page, SCRIPT))

    def test_name_prefix_is_not_a_match(self):
        # The closing quote makes the match exact: a longer name doesn't count.
        self.assertFalse(web_bundle_referenced(BUILT_INDEX_HTML, "assets/index-rt74q59x"))
        self.assertFalse(web_bundle_referenced(BUILT_INDEX_HTML.replace(".js\"", ".js.map\""), SCRIPT))

    def test_script_mentioned_outside_a_tag_is_not_referenced(self):
        self.assertFalse(web_bundle_referenced("<!-- /play/assets/index-rt74q59x.js -->", SCRIPT))


if __name__ == "__main__":
    unittest.main()
