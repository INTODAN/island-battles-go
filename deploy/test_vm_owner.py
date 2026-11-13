"""Tests for the VM owner marker (#256): once INTODAN/mpl has taken the VM over,
scripts/vm.sh deploy refuses, and a release leaves the VM alone.

scripts/vm.sh runs in a copy of the repository's deploy scripts, against a fake
AWS CLI and a fake ansible-playbook, so nothing reaches AWS or a VM.
scripts/release-plan.sh runs in a throwaway Git repository.

Run from the repository root:

    python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

import deploy_files

REPOSITORY = pathlib.Path(__file__).resolve().parent.parent
BUCKET = "island-battles-deploy-123456789012"
MARKER = deploy_files.VM_OWNER_KEY

# The AWS CLI as deploy_files.py and scripts/vm.sh call it, over a bucket kept
# in $FAKE_AWS_ROOT/objects/. Every call is appended to $FAKE_AWS_ROOT/calls.
FAKE_AWS = """\
import hashlib, json, os, pathlib, sys

root = pathlib.Path(os.environ["FAKE_AWS_ROOT"])
arguments = sys.argv[1:]
with open(root / "calls", "a") as calls:
    calls.write(json.dumps(arguments) + "\\n")
options, positional = {}, []
index = 2
while index < len(arguments):
    if arguments[index].startswith("--"):
        options[arguments[index]] = arguments[index + 1]
        index += 2
    else:
        positional.append(arguments[index])
        index += 1
objects = root / "objects"
service, operation = arguments[0], arguments[1]
if (service, operation) == ("s3api", "list-objects-v2"):
    contents = []
    for path in sorted(objects.rglob("*")):
        key = path.relative_to(objects).as_posix()
        if path.is_file() and key.startswith(options["--prefix"]):
            contents.append({"Key": key, "ETag": '"' + hashlib.md5(path.read_bytes()).hexdigest() + '"'})
    print(json.dumps({"Contents": contents} if contents else {}))
elif (service, operation) == ("s3api", "get-object"):
    source = objects / options["--key"]
    if not source.is_file():
        print("An error occurred (NoSuchKey) when calling the GetObject operation: "
              "The specified key does not exist.", file=sys.stderr)
        sys.exit(254)
    pathlib.Path(positional[0]).write_bytes(source.read_bytes())
    print("{}")
elif (service, operation) == ("s3api", "put-object"):
    target = objects / options["--key"]
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(pathlib.Path(options["--body"]).read_bytes())
    print("{}")
elif (service, operation) == ("lightsail", "get-instances"):
    print("island-battles-1")
elif (service, operation) == ("lightsail", "get-instance-access-details"):
    print(json.dumps({"accessDetails": {"privateKey": "private key", "certKey": "certificate"}}))
else:
    print(f"The fake AWS CLI can't {service} {operation}.", file=sys.stderr)
    sys.exit(2)
"""

# Records that a playbook ran, and with what.
FAKE_ANSIBLE_PLAYBOOK = """\
import json, os, pathlib, sys

with open(pathlib.Path(os.environ["FAKE_AWS_ROOT"]) / "playbooks", "a") as runs:
    runs.write(json.dumps(sys.argv[1:]) + "\\n")
"""


class VmDeployTest(unittest.TestCase):
    """scripts/vm.sh deploy, with and without the marker."""

    def setUp(self):
        self.directory = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.directory)
        self.root = self.directory / "repository"
        (self.root / "scripts").mkdir(parents=True)
        (self.root / "deploy" / "ansible").mkdir(parents=True)
        (self.root / "deploy" / "local").mkdir()
        shutil.copy(REPOSITORY / "scripts" / "vm.sh", self.root / "scripts" / "vm.sh")
        shutil.copy(REPOSITORY / "deploy" / "deploy_files.py", self.root / "deploy" / "deploy_files.py")
        (self.root / "deploy" / "local" / "vm.yml").write_text("deploy_domain: example.org\n")

        self.aws_root = self.directory / "aws"
        (self.aws_root / "objects").mkdir(parents=True)
        # The deployment runs: its state and secrets are in the bucket.
        self.put("terraform/terraform.tfstate", "{}")
        self.put("files/secrets/postgres_app_password", "p")

        self.bin = self.directory / "bin"
        self.bin.mkdir()
        self.tool("aws", FAKE_AWS)
        self.tool("ansible-playbook", FAKE_ANSIBLE_PLAYBOOK)
        self.tool("ansible-galaxy", "")

    def tool(self, name, source):
        path = self.bin / name
        path.write_text(f"#!{sys.executable}\n" + source)
        path.chmod(0o755)

    def put(self, key, content):
        path = self.aws_root / "objects" / key
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def deploy(self, **environment):
        env = {
            "PATH": os.pathsep.join([str(self.bin), str(pathlib.Path(sys.executable).parent), "/usr/bin", "/bin"]),
            "HOME": str(self.directory),
            "AWS_REGION": "eu-central-1",
            "ISLAND_BATTLES_DEPLOY_BUCKET": BUCKET,
            "FAKE_AWS_ROOT": str(self.aws_root),
            **environment,
        }
        return subprocess.run(["bash", str(self.root / "scripts" / "vm.sh"), "deploy", "203.0.113.10"],
                              cwd=self.root, env=env, capture_output=True, text=True, timeout=60)

    def playbooks(self):
        path = self.aws_root / "playbooks"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_deploys_while_the_bucket_has_no_marker(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.playbooks()), 1)
        self.assertIn("site.yml", self.playbooks()[0])

    def test_refuses_once_mpl_owns_the_vm(self):
        self.put(MARKER, "mpl\n")
        result = self.deploy()
        self.assertEqual(result.returncode, 1)
        self.assertEqual(self.playbooks(), [])
        self.assertIn("The VM is deployed from INTODAN/mpl now", result.stderr)
        self.assertIn(f"s3://{BUCKET}/mpl/vm-owner says `mpl`", result.stderr)
        self.assertIn("ISLAND_BATTLES_OVERRIDE_VM_OWNER=mpl", result.stderr)
        self.assertNotIn("::error::", result.stderr)
        # Nothing reached the VM, not even a request for its SSH key.
        calls = (self.aws_root / "calls").read_text()
        self.assertNotIn("get-instance-access-details", calls)

    def test_the_refusal_is_an_annotation_in_github_actions(self):
        self.put(MARKER, "mpl")
        result = self.deploy(GITHUB_ACTIONS="true")
        self.assertEqual(result.returncode, 1)
        self.assertIn("::error::The VM is deployed from INTODAN/mpl now", result.stderr)

    def test_the_override_deploys_anyway(self):
        self.put(MARKER, "mpl\n")
        result = self.deploy(ISLAND_BATTLES_OVERRIDE_VM_OWNER="mpl")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.playbooks()), 1)
        self.assertIn("ISLAND_BATTLES_OVERRIDE_VM_OWNER=mpl: deploying the VM", result.stderr)

    def test_an_override_must_name_the_owner(self):
        self.put(MARKER, "mpl")
        for override in ("1", "true", "island-battles"):
            with self.subTest(override=override):
                result = self.deploy(ISLAND_BATTLES_OVERRIDE_VM_OWNER=override)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(self.playbooks(), [])
                self.assertIn("The VM is deployed from INTODAN/mpl now", result.stderr)

    def test_a_marker_that_names_no_owner_refuses_too(self):
        self.put(MARKER, "")
        result = self.deploy(ISLAND_BATTLES_OVERRIDE_VM_OWNER="mpl")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(self.playbooks(), [])
        self.assertIn("names none", result.stderr)

    def test_other_keys_under_mpl_are_no_marker(self):
        self.put("mpl/tenants/island-battles/tenant.env", "DATABASE_URL=x\n")
        self.put("mpl/vm-owner-old", "mpl")
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.playbooks()), 1)


class VmOwnerTest(unittest.TestCase):
    """deploy_files.vm_owner and vm_owner_refusal on their own."""

    class Store:
        def __init__(self, marker):
            self.marker = marker

        def vm_owner_marker(self):
            return self.marker

    def test_no_marker_means_this_repository(self):
        self.assertIsNone(deploy_files.vm_owner(self.Store(None), BUCKET))
        self.assertIsNone(deploy_files.vm_owner_refusal(None, BUCKET))

    def test_the_marker_names_the_owner(self):
        self.assertEqual(deploy_files.vm_owner(self.Store("mpl\n"), BUCKET), "mpl")
        self.assertIn("INTODAN/mpl", deploy_files.vm_owner_refusal("mpl", BUCKET))
        self.assertIsNone(deploy_files.vm_owner_refusal("mpl", BUCKET, override="mpl"))
        self.assertIsNotNone(deploy_files.vm_owner_refusal("mpl", BUCKET, override="other"))

    def test_another_owner_is_named_as_it_is(self):
        self.assertIn("the repository `other`", deploy_files.vm_owner_refusal("other", BUCKET))

    def test_a_marker_without_a_name_is_an_error(self):
        for content in ("", "  \n", "mpl\nmore", "MPL", "::error::x", "x" * 65):
            with self.subTest(content=content):
                with self.assertRaises(deploy_files.AwsError):
                    deploy_files.vm_owner(self.Store(content), BUCKET)


class ReleasePlanTest(unittest.TestCase):
    """scripts/release-plan.sh with and without ISLAND_BATTLES_VM_OWNER."""

    def setUp(self):
        self.directory = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.directory)
        (self.directory / "scripts").mkdir()
        shutil.copy(REPOSITORY / "scripts" / "release-plan.sh", self.directory / "scripts" / "release-plan.sh")
        (self.directory / "deploy" / "ansible").mkdir(parents=True)
        (self.directory / "deploy" / "ansible" / "site.yml").write_text("- hosts: all\n")
        (self.directory / "README.md").write_text("Island Battles\n")
        self.env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": str(self.directory),
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_AUTHOR_NAME": "Test",
            "GIT_AUTHOR_EMAIL": "test@example.org",
            "GIT_COMMITTER_NAME": "Test",
            "GIT_COMMITTER_EMAIL": "test@example.org",
        }
        self.git("init", "-q", "-b", "main")
        self.commit("The first commit")

    def git(self, *arguments):
        subprocess.run(["git", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", *arguments],
                       cwd=self.directory, env=self.env, check=True, capture_output=True)

    def commit(self, message):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)

    def change(self, path, content):
        (self.directory / path).write_text(content)
        self.commit(f"Change {path}")

    def plan(self, bump="patch", owner=None):
        env = dict(self.env)
        if owner is not None:
            env["ISLAND_BATTLES_VM_OWNER"] = owner
        result = subprocess.run(["bash", "scripts/release-plan.sh", bump], cwd=self.directory, env=env,
                                capture_output=True, text=True, timeout=60)
        outputs = dict(line.split("=", 1) for line in result.stdout.splitlines())
        return result, outputs

    def test_changed_vm_inputs_deploy_the_vm_and_make_the_release_major(self):
        self.git("tag", "v1.2.3")
        self.change("deploy/ansible/site.yml", "- hosts: vm\n")
        result, outputs = self.plan()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs["version"], outputs["bump"], outputs["vm"], outputs["vm_owner"]),
                         ("v2.0.0", "major", "true", ""))

    def test_once_mpl_owns_the_vm_changed_inputs_neither_deploy_it_nor_make_the_release_major(self):
        self.git("tag", "v1.2.3")
        self.change("deploy/ansible/site.yml", "- hosts: vm\n")
        result, outputs = self.plan(owner="mpl")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs["version"], outputs["bump"], outputs["vm"], outputs["vm_owner"]),
                         ("v1.2.4", "patch", "false", "mpl"))
        self.assertIn("**The VM isn't deployed:** it is deployed from INTODAN/mpl now", result.stderr)
        self.assertIn("- `deploy/ansible/site.yml`", result.stderr)

    def test_once_mpl_owns_the_vm_the_bump_is_the_one_asked_for(self):
        self.git("tag", "v1.2.3")
        self.change("README.md", "Island Battles, again\n")
        result, outputs = self.plan(bump="minor", owner="mpl")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs["version"], outputs["bump"], outputs["vm"]), ("v1.3.0", "minor", "false"))
        self.assertIn("**The VM isn't deployed:**", result.stderr)

    def test_unchanged_vm_inputs_leave_the_vm_alone(self):
        self.git("tag", "v1.2.3")
        self.change("README.md", "Island Battles, again\n")
        result, outputs = self.plan()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs["version"], outputs["bump"], outputs["vm"]), ("v1.2.4", "patch", "false"))
        self.assertNotIn("The VM isn't deployed", result.stderr)

    def test_the_first_release_leaves_a_vm_that_mpl_owns_alone(self):
        result, outputs = self.plan(owner="mpl")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs["version"], outputs["vm"]), ("v0.1.0", "false"))
        result, outputs = self.plan()
        self.assertEqual((outputs["version"], outputs["vm"]), ("v0.1.0", "true"))

    def test_rejects_an_owner_that_isnt_a_name(self):
        result, _ = self.plan(owner="mpl\nvm=true")
        self.assertEqual(result.returncode, 2)
        self.assertIn("ISLAND_BATTLES_VM_OWNER must name a repository", result.stderr)


class WorkflowTest(unittest.TestCase):
    """The workflows read the marker where this repository deploys the VM."""

    def test_the_deploy_workflows_vm_target_checks_the_owner_first(self):
        workflow = (REPOSITORY / ".github" / "workflows" / "deploy.yml").read_text()
        check = workflow.index("deploy_files.py\" check-vm-owner")
        self.assertLess(check, workflow.index('scripts/vm.sh deploy "$(scripts/aws.sh output static_ip)"'))
        self.assertLess(workflow.index("aws-actions/configure-aws-credentials"), check)

    def test_the_release_plans_with_the_owner(self):
        workflow = (REPOSITORY / ".github" / "workflows" / "release.yml").read_text()
        self.assertIn("python3 -I deploy/deploy_files.py vm-owner", workflow)
        self.assertIn("ISLAND_BATTLES_VM_OWNER: ${{ needs.vm_owner.outputs.owner }}", workflow)
        self.assertIn("if: needs.plan.outputs.vm == 'true'", workflow)


if __name__ == "__main__":
    unittest.main()
