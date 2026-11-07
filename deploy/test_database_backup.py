"""Tests for the VM's daily backup script, with fake commands and AWS clients.

Run with: python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import contextlib
import datetime
import importlib.machinery
import importlib.util
import io
import os
import pathlib
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).parent / "ansible" / "roles" / "app" / "files" / "island-battles-backup"


def load_script():
    loader = importlib.machinery.SourceFileLoader("island_battles_backup", str(SCRIPT))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


backup = load_script()
NOW = datetime.datetime(2026, 11, 8, 1, 30, 5, tzinfo=datetime.timezone.utc)
DUMP_NAME = "island_battles-20261108T013005Z.dump"


class FakeDocker:
    """Answers the table count, and writes a dump where the db container's /dumps is."""

    def __init__(self, app_dir, tables="3", fail_dump=False):
        self.app_dir = app_dir
        self.tables = tables
        self.fail_dump = fail_dump
        self.commands = []

    def __call__(self, command, **options):
        self.commands.append(command)
        assert options["cwd"] == self.app_dir
        assert command[:5] == ["docker", "compose", "exec", "-T", "db"]
        if command[5] == "psql":
            return subprocess.CompletedProcess(command, 0, stdout=f"{self.tables}\n", stderr="")
        if command[5] == "pg_dump":
            if self.fail_dump:
                raise subprocess.CalledProcessError(1, command)
            target = command[command.index("--file") + 1]
            self.dumped = os.path.join(self.app_dir, "dumps", os.path.basename(target))
            with open(self.dumped, "wb") as dump:
                dump.write(b"PGDMP fake")
            return subprocess.CompletedProcess(command, 0)
        raise AssertionError(f"unexpected command {command}")


class FakeS3:
    def __init__(self, error=None):
        self.error = error
        self.uploads = []

    def put_object(self, **arguments):
        if self.error:
            raise self.error
        arguments["Body"] = arguments["Body"].read()
        self.uploads.append(arguments)


class FakeCloudWatch:
    def __init__(self, error=None):
        self.error = error
        self.metrics = []

    def put_metric_data(self, **arguments):
        if self.error:
            raise self.error
        self.metrics.append(arguments)


class BackupTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.app_dir = directory.name
        os.mkdir(os.path.join(self.app_dir, "dumps"))
        self.environ = {
            "ISLAND_BATTLES_APP_DIR": self.app_dir,
            "ISLAND_BATTLES_DATABASE": "island_battles",
            "ISLAND_BATTLES_MOVED_AWAY_MARKER": os.path.join(self.app_dir, "moved-away"),
            "ISLAND_BATTLES_BACKUP_BUCKET": "island-battles-backups-123456789012",
            "ISLAND_BATTLES_BACKUP_PREFIX": "postgres/",
            "ISLAND_BATTLES_BACKUP_REGION": "eu-central-1",
            "ISLAND_BATTLES_BACKUP_METRIC_NAMESPACE": "IslandBattles/VM",
            "ISLAND_BATTLES_BACKUP_METRIC_NAME": "DatabaseBackups",
        }
        self.s3 = FakeS3()
        self.cloudwatch = FakeCloudWatch()
        self.regions = []

    def clients(self, region):
        self.regions.append(region)
        return self.s3, self.cloudwatch

    def run_backup(self, docker):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            status = backup.main(environ=self.environ, run=docker, clients=self.clients, now=NOW)
        return status, output

    def test_uploads_the_dump_and_counts_it(self):
        docker = FakeDocker(self.app_dir)
        status, output = self.run_backup(docker)

        self.assertEqual(status, 0, output.getvalue())
        self.assertEqual(self.regions, ["eu-central-1"])
        self.assertEqual(self.s3.uploads, [{
            "Bucket": "island-battles-backups-123456789012",
            "Key": f"postgres/{DUMP_NAME}",
            "Body": b"PGDMP fake",
            "ContentType": "application/octet-stream",
        }])
        self.assertEqual(self.cloudwatch.metrics, [{
            "Namespace": "IslandBattles/VM",
            "MetricData": [{"MetricName": "DatabaseBackups", "Value": 1, "Unit": "Count"}],
        }])
        dump_command = docker.commands[-1]
        self.assertEqual(dump_command[5:], [
            "pg_dump", "--username", "postgres", "--dbname", "island_battles",
            "--format", "custom", "--no-owner", "--no-privileges", "--file", f"/dumps/{DUMP_NAME}",
        ])
        self.assertEqual(os.listdir(os.path.join(self.app_dir, "dumps")), [], "the dump must not stay on the VM")
        self.assertIn(f"s3://island-battles-backups-123456789012/postgres/{DUMP_NAME}", output.getvalue())

    def test_skips_a_vm_that_was_moved_away_from(self):
        open(self.environ["ISLAND_BATTLES_MOVED_AWAY_MARKER"], "w").close()
        docker = FakeDocker(self.app_dir)
        status, output = self.run_backup(docker)

        self.assertEqual(status, 0)
        self.assertEqual(docker.commands, [])
        self.assertEqual((self.s3.uploads, self.cloudwatch.metrics), ([], []))
        self.assertIn("moved away", output.getvalue())

    def test_skips_a_database_without_tables(self):
        status, output = self.run_backup(FakeDocker(self.app_dir, tables="0"))

        self.assertEqual(status, 0)
        self.assertEqual((self.s3.uploads, self.cloudwatch.metrics), ([], []))
        self.assertIn("no tables", output.getvalue())

    def test_fails_without_counting_when_pg_dump_fails(self):
        status, output = self.run_backup(FakeDocker(self.app_dir, fail_dump=True))

        self.assertEqual(status, 1)
        self.assertEqual((self.s3.uploads, self.cloudwatch.metrics), ([], []))
        self.assertIn("pg_dump failed", output.getvalue())

    def test_fails_without_counting_and_removes_the_dump_when_the_upload_fails(self):
        self.s3 = FakeS3(error=RuntimeError("AccessDenied"))
        status, output = self.run_backup(FakeDocker(self.app_dir))

        self.assertEqual(status, 1)
        self.assertEqual(self.cloudwatch.metrics, [])
        self.assertEqual(os.listdir(os.path.join(self.app_dir, "dumps")), [])
        self.assertIn("didn't reach", output.getvalue())

    def test_says_the_backup_is_safe_when_only_counting_fails(self):
        self.cloudwatch = FakeCloudWatch(error=RuntimeError("Throttling"))
        status, output = self.run_backup(FakeDocker(self.app_dir))

        self.assertEqual(status, 1)
        self.assertEqual(len(self.s3.uploads), 1)
        self.assertIn("is in S3, but counting it failed", output.getvalue())

    def test_refuses_to_run_without_its_settings(self):
        del self.environ["ISLAND_BATTLES_BACKUP_BUCKET"]
        docker = FakeDocker(self.app_dir)
        status, output = self.run_backup(docker)

        self.assertEqual(status, 1)
        self.assertEqual(docker.commands, [])
        self.assertIn("ISLAND_BATTLES_BACKUP_BUCKET", output.getvalue())


if __name__ == "__main__":
    unittest.main()
