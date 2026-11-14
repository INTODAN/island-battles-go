"""Tests for the backend Lambda's settings, which scripts/lambda-env.sh writes
with deploy/ansible/lambda-env.yml from INTODAN/mpl's settings for the tenant
(#232).

The playbook's tests run it on a copy of deploy/ansible with fake settings,
when ansible-playbook is installed (`uv tool install ansible-core`).

Run with: python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import importlib.util
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ANSIBLE = pathlib.Path(__file__).parent / "ansible"

TENANT = """# Written by INTODAN/mpl's VM deploy.
DATABASE_URL=postgres://island_battles:database-password@vm.example.org:5432/island_battles?sslmode=require
DATABASE_HOST=vm.example.org
LOBBY_URL=https://lobby.example.org
LOBBY_GAME_KEY=island-battles
LOBBY_GAME_SECRET=game-secret-of-at-least-thirty-two-bytes
CENTRIFUGO_API_URL=https://vm.example.org/api
CENTRIFUGO_API_KEY=centrifugo-api-key
CENTRIFUGO_PUBLIC_URL=wss://vm.example.org/connection/websocket
"""


def load_filter():
    spec = importlib.util.spec_from_file_location("env_file", ANSIBLE / "filter_plugins" / "env_file.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.env_file


class EnvFileFilterTest(unittest.TestCase):
    def test_reads_key_value_lines_literally(self):
        env_file = load_filter()
        self.assertEqual(env_file("# comment\n\nA=b=c\nC=\nA=d\n"), {"A": "d", "C": ""})
        self.assertEqual(env_file("URL=postgres://a:b@c:5432/d?x=y\n"), {"URL": "postgres://a:b@c:5432/d?x=y"})

    def test_refuses_a_line_that_isnt_key_value(self):
        with self.assertRaisesRegex(ValueError, "line 2"):
            load_filter()("A=b\nexport C=d\n")


@unittest.skipUnless(shutil.which("ansible-playbook"), "ansible-playbook is not installed")
class LambdaEnvPlaybookTest(unittest.TestCase):
    def setUp(self):
        self.root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.root)
        shutil.copytree(ANSIBLE, self.root / "deploy" / "ansible")
        self.local = self.root / "deploy" / "local"
        (self.local / "secrets").mkdir(parents=True)
        (self.local / "secrets" / "admin_api_key").write_text("admin-api-key\n")
        (self.local / "secrets" / "submit_token_secret").write_text("submit-token-secret\n")
        (self.local / "tenant.env").write_text(TENANT)

    def run_playbook(self, *options):
        return subprocess.run(["ansible-playbook", "lambda-env.yml", *options], cwd=self.root / "deploy" / "ansible",
                              capture_output=True, text=True)

    def settings(self):
        lines = (self.local / "lambda.env").read_text().splitlines()
        return dict(line.split("=", 1) for line in lines if line and not line.startswith("#"))

    def test_writes_the_backends_settings_from_the_tenants(self):
        result = self.run_playbook()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.settings(), {
            "ISLAND_BATTLES_DATABASE_URL": "postgres://island_battles:database-password@vm.example.org:5432/island_battles?sslmode=require",
            "ISLAND_BATTLES_CENTRIFUGO_API_URL": "https://vm.example.org/api",
            "ISLAND_BATTLES_CENTRIFUGO_API_KEY": "centrifugo-api-key",
            "ISLAND_BATTLES_LOBBY_URL": "https://lobby.example.org",
            "ISLAND_BATTLES_LOBBY_HOOK_SECRET": "game-secret-of-at-least-thirty-two-bytes",
            "ISLAND_BATTLES_ADMIN_API_KEY": "admin-api-key",
            "ISLAND_BATTLES_SUBMIT_TOKEN_SECRET": "submit-token-secret",
        })
        self.assertEqual((self.local / "lambda.env").stat().st_mode & 0o777, 0o600)
        # The secrets never reach the output.
        for secret in ("database-password", "game-secret-of", "centrifugo-api-key", "admin-api-key", "submit-token-secret"):
            self.assertNotIn(secret, result.stdout + result.stderr)

    def test_refuses_another_games_settings(self):
        (self.local / "tenant.env").write_text(TENANT.replace("LOBBY_GAME_KEY=island-battles", "LOBBY_GAME_KEY=other"))
        result = self.run_playbook()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("isn't the settings of the tenant island-battles", result.stdout)
        self.assertFalse((self.local / "lambda.env").exists())

    def test_names_what_the_tenants_settings_lack(self):
        (self.local / "tenant.env").write_text(TENANT.replace("CENTRIFUGO_API_KEY=centrifugo-api-key\n", ""))
        result = self.run_playbook()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing: CENTRIFUGO_API_KEY", result.stdout)

    def test_needs_the_tenants_settings(self):
        (self.local / "tenant.env").unlink()
        result = self.run_playbook()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("mpl/tenants/island-battles/tenant.env", result.stdout)

    def test_keeps_a_running_deployments_secrets(self):
        (self.local / "secrets" / "submit_token_secret").unlink()
        result = self.run_playbook()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("lacks submit_token_secret", result.stdout)
        self.assertFalse((self.local / "secrets" / "submit_token_secret").exists())

    def test_a_new_deployment_generates_its_secrets(self):
        (self.local / "secrets" / "submit_token_secret").unlink()
        result = self.run_playbook("-e", "app_generate_secrets=true")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(len(self.settings()["ISLAND_BATTLES_SUBMIT_TOKEN_SECRET"]), 64)


if __name__ == "__main__":
    unittest.main()
