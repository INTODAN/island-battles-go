"""Runs the web client's CloudFront Function (web_client.tf) in node."""

import json
import pathlib
import re
import shutil
import subprocess
import unittest

SOURCE = pathlib.Path(__file__).parent / "terraform" / "web_client.tf"


def function_code():
    text = SOURCE.read_text()
    match = re.search(r"code\s*=\s*<<-EOT\n(.*?)\n\s*EOT\n", text, re.S)
    assert match, "no function code in web_client.tf"
    return match.group(1)


@unittest.skipUnless(shutil.which("node"), "node is not installed")
class WebClientFunctionTest(unittest.TestCase):
    def run_function(self, uri, querystring=None):
        script = (
            function_code()
            + "\nconsole.log(JSON.stringify(handler({request: "
            + json.dumps({"uri": uri, "querystring": querystring or {}})
            + "})));"
        )
        out = subprocess.run(
            ["node", "-e", script], capture_output=True, text=True, check=True
        )
        return json.loads(out.stdout)

    def location(self, querystring=None):
        response = self.run_function("/play", querystring)
        self.assertEqual(response["statusCode"], 301)
        return response["headers"]["location"]["value"]

    def test_redirect_without_query(self):
        self.assertEqual(self.location(), "/play/")

    def test_redirect_keeps_query(self):
        self.assertEqual(self.location({"join": {"value": "ABC"}}), "/play/?join=ABC")

    def test_redirect_encodes_values(self):
        query = {"join": {"value": "A%20B"}, "x": {"value": "1"}}
        self.assertEqual(self.location(query), "/play/?join=A%20B&x=1")
        self.assertEqual(self.location({"q": {"value": "a&b=c"}}), "/play/?q=a%26b%3Dc")

    def test_redirect_multi_value(self):
        query = {"k": {"value": "1", "multiValue": [{"value": "1"}, {"value": "2"}]}}
        self.assertEqual(self.location(query), "/play/?k=1&k=2")

    def test_redirect_valueless_key(self):
        self.assertEqual(self.location({"flag": {"value": ""}}), "/play/?flag")

    def test_directory_serves_index(self):
        query = {"join": {"value": "ABC"}}
        request = self.run_function("/play/", query)
        self.assertEqual(request["uri"], "/play/index.html")
        self.assertEqual(request["querystring"], query)

    def test_assets_unchanged(self):
        request = self.run_function("/play/assets/index-1a2b3c.js")
        self.assertEqual(request["uri"], "/play/assets/index-1a2b3c.js")


if __name__ == "__main__":
    unittest.main()
