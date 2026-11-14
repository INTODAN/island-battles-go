"""Runs the lobby redirect's CloudFront Function (cloudfront.tf) in node."""

import json
import pathlib
import re
import shutil
import subprocess
import unittest

SOURCE = pathlib.Path(__file__).parent / "terraform" / "cloudfront.tf"
LOBBY_HOSTNAME = "lobby.example.org"


def function_code():
    text = SOURCE.read_text()
    match = re.search(
        r'resource "aws_cloudfront_function" "lobby_redirect".*?code\s*=\s*<<-EOT\n(.*?)\n\s*EOT\n', text, re.S
    )
    assert match, "no lobby_redirect function code in cloudfront.tf"
    code = match.group(1)
    assert "${var.lobby_hostname}" in code, "the function doesn't name lobby_hostname"
    return code.replace("${var.lobby_hostname}", LOBBY_HOSTNAME)


@unittest.skipUnless(shutil.which("node"), "node is not installed")
class LobbyRedirectFunctionTest(unittest.TestCase):
    def location(self, uri, querystring=None):
        script = (
            function_code()
            + "\nconsole.log(JSON.stringify(handler({request: "
            + json.dumps({"uri": uri, "querystring": querystring or {}})
            + "})));"
        )
        out = subprocess.run(["node", "-e", script], capture_output=True, text=True, check=True)
        response = json.loads(out.stdout)
        self.assertEqual(response["statusCode"], 301)
        return response["headers"]["location"]["value"]

    def test_the_page_moves_to_the_lobby(self):
        self.assertEqual(self.location("/lobby/ui/"), "https://lobby.example.org/lobby/ui/")

    def test_the_page_without_its_slash_moves_to_the_page(self):
        self.assertEqual(self.location("/lobby/ui"), "https://lobby.example.org/lobby/ui/")

    def test_files_under_the_page_keep_their_path(self):
        self.assertEqual(self.location("/lobby/ui/app.js"), "https://lobby.example.org/lobby/ui/app.js")

    def test_the_query_string_is_kept(self):
        query = {"join": {"value": "A%20B"}, "flag": {"value": ""}}
        self.assertEqual(self.location("/lobby/ui/", query), "https://lobby.example.org/lobby/ui/?join=A%20B&flag")

    def test_repeated_keys_are_kept(self):
        query = {"k": {"value": "1", "multiValue": [{"value": "1"}, {"value": "2"}]}}
        self.assertEqual(self.location("/lobby/ui/", query), "https://lobby.example.org/lobby/ui/?k=1&k=2")


if __name__ == "__main__":
    unittest.main()
