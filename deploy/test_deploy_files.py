"""Tests for deploy_files.py's sync, against an in-memory bucket.

Run from the repository root:

    python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import hashlib
import pathlib
import shutil
import tempfile
import unittest

import deploy_files

BUCKET = "island-battles-deploy-123456789012"


def md5(content):
    return hashlib.md5(content.encode(), usedforsecurity=False).hexdigest()


class FakeStore:
    """The bucket's files/, with the same conditional uploads as S3."""

    def __init__(self, files=None, state=False):
        self.files = dict(files or {})
        self.state = state
        self.uploads = []

    def list(self):
        return {path: md5(content) for path, content in self.files.items()}

    def download(self, path, destination):
        pathlib.Path(destination).write_text(self.files[path])

    def upload(self, path, source, expected):
        current = self.files.get(path)
        if (None if current is None else md5(current)) != expected:
            raise deploy_files.PreconditionFailed("412")
        self.files[path] = pathlib.Path(source).read_text()
        self.uploads.append(path)

    def has_state(self):
        return self.state


class SyncTest(unittest.TestCase):
    def setUp(self):
        self.directory = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.directory)

    def write(self, path, content):
        target = self.directory / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)

    def read(self, path):
        return (self.directory / path).read_text()

    def pull(self, store, force=False):
        return deploy_files.pull(store, self.directory, BUCKET, force, log=lambda _: None)

    def push(self, store, force=False):
        return deploy_files.push(store, self.directory, BUCKET, force, log=lambda _: None)

    def test_a_new_machine_pulls_everything(self):
        store = FakeStore({"vm.yml": "a", "secrets/centrifugo_api_key": "k"})
        self.assertEqual(self.pull(store), [])
        self.assertEqual(self.read("vm.yml"), "a")
        self.assertEqual(self.read("secrets/centrifugo_api_key"), "k")
        self.assertEqual((self.directory / "secrets/centrifugo_api_key").stat().st_mode & 0o777, 0o600)

    def test_the_first_push_uploads_everything_shared(self):
        self.write("vm.yml", "a")
        self.write("secrets/postgres_app_password", "p")
        self.write("terraform/terraform.tfstate", "old state")
        self.write("lambda/bootstrap", "binary")
        self.write("dumps/island_battles.dump", "dump")
        store = FakeStore()
        self.assertEqual(self.push(store), [])
        self.assertEqual(store.files, {"vm.yml": "a", "secrets/postgres_app_password": "p"})

    def test_pull_takes_a_change_made_only_in_the_bucket(self):
        store = FakeStore({"vm.yml": "a"})
        self.pull(store)
        store.files["vm.yml"] = "b"
        self.assertEqual(self.pull(store), [])
        self.assertEqual(self.read("vm.yml"), "b")

    def test_pull_keeps_a_change_made_only_here_and_push_sends_it(self):
        store = FakeStore({"vm.yml": "a"})
        self.pull(store)
        self.write("vm.yml", "mine")
        self.assertEqual(self.pull(store), [])
        self.assertEqual(self.read("vm.yml"), "mine")
        self.assertEqual(self.push(store), [])
        self.assertEqual(store.files["vm.yml"], "mine")

    def test_pull_restores_a_deleted_local_file(self):
        store = FakeStore({"vm.yml": "a"})
        self.pull(store)
        (self.directory / "vm.yml").unlink()
        self.assertEqual(self.pull(store), [])
        self.assertEqual(self.read("vm.yml"), "a")

    def test_changes_on_both_sides_conflict_until_forced(self):
        store = FakeStore({"aws.tfvars": "a"})
        self.pull(store)
        store.files["aws.tfvars"] = "theirs"
        self.write("aws.tfvars", "mine")
        self.assertEqual(self.pull(store), ["aws.tfvars"])
        self.assertEqual(self.read("aws.tfvars"), "mine")
        self.assertEqual(self.push(store), ["aws.tfvars"])
        self.assertEqual(store.files["aws.tfvars"], "theirs")
        self.assertEqual(self.push(store, force=True), [])
        self.assertEqual(store.files["aws.tfvars"], "mine")

    def test_pull_force_takes_the_buckets_copy(self):
        store = FakeStore({"aws.tfvars": "a"})
        self.pull(store)
        store.files["aws.tfvars"] = "theirs"
        self.write("aws.tfvars", "mine")
        self.assertEqual(self.pull(store, force=True), [])
        self.assertEqual(self.read("aws.tfvars"), "theirs")

    def test_a_machine_that_never_synced_conflicts_on_a_different_file(self):
        self.write("secrets/postgres_app_password", "generated here")
        store = FakeStore({"secrets/postgres_app_password": "the live one"})
        self.assertEqual(self.pull(store), ["secrets/postgres_app_password"])
        self.assertEqual(self.push(store), ["secrets/postgres_app_password"])
        self.assertEqual(store.files["secrets/postgres_app_password"], "the live one")

    def test_push_leaves_a_newer_bucket_copy_alone(self):
        store = FakeStore({"lambda.env": "a"})
        self.pull(store)
        store.files["lambda.env"] = "b"
        self.assertEqual(self.push(store), [])
        self.assertEqual(store.files["lambda.env"], "b")
        self.assertEqual(store.uploads, [])

    def test_push_refuses_to_bring_back_a_file_the_bucket_lost(self):
        store = FakeStore({"smoke-session.json": "a"})
        self.pull(store)
        del store.files["smoke-session.json"]
        self.assertEqual(self.push(store), ["smoke-session.json"])
        self.assertEqual(self.push(store, force=True), [])
        self.assertEqual(store.files["smoke-session.json"], "a")

    def test_a_push_that_loses_a_race_is_a_conflict(self):
        store = FakeStore({"lambda.env": "a"})
        self.pull(store)
        self.write("lambda.env", "mine")
        listed = store.list()
        store.list = lambda: listed
        store.files["lambda.env"] = "pushed meanwhile"
        self.assertEqual(self.push(store), ["lambda.env"])
        self.assertEqual(store.files["lambda.env"], "pushed meanwhile")

    def test_new_secrets_from_a_vm_deploy_are_pushed(self):
        store = FakeStore({"secrets/postgres_app_password": "p"})
        self.pull(store)
        self.write("secrets/postgres_admin_password", "new")
        self.assertEqual(self.push(store), [])
        self.assertEqual(store.files["secrets/postgres_admin_password"], "new")

    def test_a_base_from_another_bucket_counts_for_nothing(self):
        store = FakeStore({"vm.yml": "a"})
        self.pull(store)
        self.write("vm.yml", "mine")
        other = FakeStore({"vm.yml": "other"})
        conflicts = deploy_files.pull(other, self.directory, "another-bucket", log=lambda _: None)
        self.assertEqual(conflicts, ["vm.yml"])


class RequireSecretsTest(unittest.TestCase):
    def test_a_new_deployment_may_generate_its_secrets(self):
        self.assertTrue(deploy_files.require_secrets(FakeStore(), BUCKET))

    def test_a_running_deployment_needs_its_secrets_in_the_bucket(self):
        self.assertFalse(deploy_files.require_secrets(FakeStore({"vm.yml": "a"}, state=True), BUCKET))
        self.assertTrue(deploy_files.require_secrets(FakeStore({"secrets/admin_api_key": "k"}, state=True), BUCKET))


if __name__ == "__main__":
    unittest.main()
