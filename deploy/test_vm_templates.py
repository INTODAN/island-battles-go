"""Tests for the VM deploy's templates and settings that give the lobby its own
database (#229), rendered with plain substitutions, as Ansible would.

Run with: python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import pathlib
import re
import unittest

ANSIBLE = pathlib.Path(__file__).parent / "ansible"
TEMPLATES = ANSIBLE / "roles" / "app" / "templates"

SETTINGS = {
    "ansible_managed": "Ansible managed",
    "postgres_database": "island_battles",
    "postgres_app_role": "island_battles",
    "postgres_app_password": "game-password",
    "postgres_admin_role": "island_battles_admin",
    "postgres_admin_password": "admin-password",
    "lobby_database": "lobby",
    "lobby_app_role": "lobby",
    "lobby_app_password": "lobby-password",
    "vm_hostname": "vm.example.org",
    "vm_app_dir": "/opt/island-battles",
    "app_postgres_published_port": "5432",
    "app_moved_away_marker": "/opt/island-battles/moved-away",
    "app_backup_settings_dir": "/etc/island-battles",
    "app_game_origin": "https://island-battles.example.org",
    "centrifugo_api_key": "api-key",
    "connection_token_hmac_secret": "token-secret",
    "lobby_proxy_secret": "proxy-secret",
    "admin_api_key": "admin-key",
    "submit_token_secret": "submit-secret",
    "lobby_player_token_keys": "kid:seed",
    "lobby_game_secret_island_battles": "hook-secret",
    "app_database_backups.bucket": "backups",
    "app_database_backups.prefix": "postgres/",
    "app_database_backups.region": "eu-central-1",
    "app_database_backups.metric_namespace": "IslandBattles/VM",
    "app_database_backups.metric_name": "DatabaseBackups",
    "app_database_backups.access_key_id": "AKIA",
    "app_database_backups.secret_access_key": "secret",
}


def render(name):
    """Render a template that uses only {{ name }} substitutions."""
    template = (TEMPLATES / name).read_text()
    if "{%" in template:
        raise AssertionError(f"{name} uses more than plain substitutions")

    def substitute(match):
        return SETTINGS[match.group(1)]

    return re.sub(r"\{\{ ([a-z_.]+) \}\}", substitute, template)


def settings(text):
    """KEY=value lines, as scripts/aws.sh and Terraform read lambda.env."""
    return dict(line.split("=", 1) for line in text.splitlines() if line and not line.startswith("#"))


class LambdaSettingsTest(unittest.TestCase):
    def test_the_lambda_gets_both_databases(self):
        values = settings(render("lambda.env.j2"))
        self.assertEqual(
            values["ISLAND_BATTLES_DATABASE_URL"],
            "postgres://island_battles:game-password@vm.example.org:5432/island_battles?sslmode=require",
        )
        self.assertEqual(
            values["LOBBY_DATABASE_URL"],
            "postgres://lobby:lobby-password@vm.example.org:5432/lobby?sslmode=require",
        )


class ClientAuthenticationTest(unittest.TestCase):
    def rules(self):
        rules = set()
        for line in render("pg_hba.conf.j2").splitlines():
            fields = line.split()
            if fields and fields[0] == "hostssl":
                self.assertEqual(fields[4], "scram-sha-256", line)
                rules.add((fields[1], fields[2], fields[3]))
        return rules

    def test_each_role_reaches_only_its_own_database_over_tls(self):
        rules = self.rules()
        for database, role in (("island_battles", "island_battles"), ("lobby", "lobby"),
                               ("island_battles", "island_battles_admin"), ("lobby", "island_battles_admin")):
            for network in ("0.0.0.0/0", "::/0"):
                self.assertIn((database, role, network), rules)
        self.assertNotIn("lobby", {role for database, role, _ in rules if database == "island_battles"})
        self.assertNotIn("island_battles", {role for database, role, _ in rules if database == "lobby"})
        self.assertEqual(len(rules), 8)

    def test_nothing_connects_without_tls(self):
        for line in render("pg_hba.conf.j2").splitlines():
            fields = line.split()
            if fields and not line.startswith("#"):
                self.assertIn(fields[0], ("local", "hostssl"), line)


class BackupSettingsTest(unittest.TestCase):
    def test_the_backup_covers_both_databases(self):
        values = settings(render("backup.env.j2"))
        self.assertEqual(values["ISLAND_BATTLES_DATABASES"].split(), ["island_battles", "lobby"])
        self.assertNotIn("ISLAND_BATTLES_DATABASE", values)

    def test_the_service_names_both_databases(self):
        service = render("island-battles-backup.service.j2")
        self.assertIn("Description=Back up the island_battles and lobby databases to S3", service)


class AdminRoleTest(unittest.TestCase):
    def test_the_role_connects_to_both_databases(self):
        role = render("admin-role.sql.j2")
        self.assertIn('GRANT CONNECT ON DATABASE "island_battles" TO "island_battles_admin";', role)
        self.assertIn('GRANT CONNECT ON DATABASE "lobby" TO "island_battles_admin";', role)
        self.assertNotIn("GRANT SELECT", role)

    def test_each_database_hides_bearer_hashes(self):
        for name in ("admin-grants-game.sql.j2", "admin-grants-lobby.sql.j2"):
            grants = render(name)
            self.assertIn("GRANT SELECT (id, player_id, created_at, expires_at) ON public.lobby_sessions", grants, name)
            self.assertIn("REVOKE EXECUTE ON FUNCTION", grants, name)
        # The lobby's default privileges follow the lobby's role, and only
        # once lobby_sessions exists.
        lobby = render("admin-grants-lobby.sql.j2")
        self.assertIn('ALTER DEFAULT PRIVILEGES FOR ROLE "lobby" IN SCHEMA public', lobby)
        self.assertLess(lobby.index("IF to_regclass('public.lobby_sessions') IS NOT NULL"), lobby.index("ALTER DEFAULT PRIVILEGES"))
        self.assertIn('ALTER DEFAULT PRIVILEGES FOR ROLE "island_battles" IN SCHEMA public', render("admin-grants-game.sql.j2"))


class SettingsTest(unittest.TestCase):
    def test_the_lobby_has_its_own_database_role_and_password(self):
        group_vars = (ANSIBLE / "group_vars" / "all.yml").read_text()
        self.assertRegex(group_vars, r"(?m)^lobby_database: lobby$")
        self.assertRegex(group_vars, r"(?m)^lobby_app_role: lobby$")
        self.assertRegex(group_vars, r"deploy_secrets_dir ~ '/postgres_lobby_password'")

    def test_a_deploy_needs_the_lobbys_password(self):
        role_vars = (ANSIBLE / "roles" / "app" / "vars" / "main.yml").read_text()
        self.assertIn('"{{ deploy_secrets_dir }}/postgres_lobby_password"', role_vars)


if __name__ == "__main__":
    unittest.main()
