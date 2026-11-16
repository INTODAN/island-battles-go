"""Tests for the role GitHub Actions applies Terraform with, with a fake AWS CLI.

Run with: python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import contextlib
import io
import json
import os
import pathlib
import re
import unittest
from unittest import mock

import deploy_files
import github_apply_role
from deploy_files import AwsError

ACCOUNT = "123456789012"
REPOSITORY = "INTODAN/island-battles-go"
DEPLOY_BUCKET = f"island-battles-deploy-{ACCOUNT}"
DEPLOY = f"arn:aws:s3:::{DEPLOY_BUCKET}"
# The buckets that deploy/terraform/ creates: backups.tf, web_client.tf and
# bot_packages.tf.
TERRAFORM_BUCKETS = [f"island-battles-backups-{ACCOUNT}", f"island-battles-web-client-{ACCOUNT}",
                     "island-battles-bot-packages"]
# What INTODAN/mpl keeps in the deploy bucket.
MPL_KEYS = ["mpl/terraform/terraform.tfstate", "mpl/files/secrets/x", "mpl/tenants/island-battles/tenant.env",
            "mpl/vm-owner"]
# S3 actions to ask the policy about: the object actions an apply could use,
# and the bucket-level writes it mustn't.
S3_ACTIONS = [
    "s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:GetObjectVersion", "s3:DeleteObjectVersion",
    "s3:PutObjectAcl", "s3:PutObjectTagging", "s3:ListBucket", "s3:ListBucketVersions", "s3:CreateBucket",
    "s3:DeleteBucket", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutBucketVersioning",
    "s3:PutLifecycleConfiguration", "s3:PutEncryptionConfiguration", "s3:PutBucketPublicAccessBlock",
    "s3:PutBucketTagging",
]


# Every action that reads an object, which ReadOnlyAccess allows with s3:Get*:
# its content, earlier versions, checksums, tags, ACLs and locks.
OBJECT_READS = [
    "s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectAttributes", "s3:GetObjectVersionAttributes",
    "s3:GetObjectTorrent", "s3:GetObjectVersionTorrent", "s3:GetObjectVersionForReplication",
    "s3:GetObjectTagging", "s3:GetObjectVersionTagging", "s3:GetObjectAcl", "s3:GetObjectVersionAcl",
    "s3:GetObjectRetention", "s3:GetObjectLegalHold",
]
# ReadOnlyAccess's S3 part, which the role has besides its inline policy.
READ_ONLY_ACCESS_S3 = {"Sid": "ReadOnlyAccess", "Effect": "Allow", "Action": ["s3:Get*", "s3:List*"], "Resource": "*"}


def listed(value):
    return value if isinstance(value, list) else [value]


def wildcard_matches(pattern, value, ignore_case=False):
    """Whether value matches pattern as IAM matches them: * is any run of
    characters, / included, ? is any one character, and nothing else is
    special. Actions ignore case; resources don't."""
    expression = "".join(".*" if character == "*" else "." if character == "?" else re.escape(character)
                         for character in pattern)
    return re.fullmatch(expression, value, re.DOTALL | (re.IGNORECASE if ignore_case else 0)) is not None


def matches(statement, action, resource):
    """Whether statement applies to action on resource, with Action or
    NotAction and Resource or NotResource. It refuses a Condition, which it
    can't evaluate: a conditional Deny taken as unconditional would make a
    test pass that shouldn't."""
    if "Condition" in statement:
        raise ValueError(f"{statement.get('Sid')}: these tests can't evaluate a Condition")
    if ("Action" in statement) == ("NotAction" in statement) or \
            ("Resource" in statement) == ("NotResource" in statement):
        raise ValueError(f"{statement.get('Sid')} needs one of Action and NotAction, and one of Resource and NotResource")
    if "Action" in statement:
        action_matches = any(wildcard_matches(pattern, action, True) for pattern in listed(statement["Action"]))
    else:
        action_matches = not any(wildcard_matches(pattern, action, True) for pattern in listed(statement["NotAction"]))
    if "Resource" in statement:
        resource_matches = any(wildcard_matches(pattern, resource) for pattern in listed(statement["Resource"]))
    else:
        resource_matches = not any(wildcard_matches(pattern, resource) for pattern in listed(statement["NotResource"]))
    return action_matches and resource_matches


def allows(policy, action, resource):
    """Whether the policy allows action on resource: an Allow names it, and
    no Deny does. Without with_read_only_access, that's the inline policy alone."""
    statements = policy["Statement"]
    return (any(statement["Effect"] == "Allow" and matches(statement, action, resource) for statement in statements)
            and not any(statement["Effect"] == "Deny" and matches(statement, action, resource)
                        for statement in statements))


def with_read_only_access(policy):
    """The role's whole S3 permissions: the inline policy and ReadOnlyAccess."""
    return {**policy, "Statement": [*policy["Statement"], READ_ONLY_ACCESS_S3]}


def allowed_s3_actions(policy, resource):
    return {action for action in S3_ACTIONS if allows(policy, action, resource)}


class FakeAws:
    """Records `aws` calls and answers the few that return something."""

    def __init__(self, role_exists=False, provider_exists=True):
        self.role_exists = role_exists
        self.provider_exists = provider_exists
        self.calls = []

    def text(self, *arguments):
        self.calls.append(arguments)
        if arguments[:2] == ("sts", "get-caller-identity"):
            return ACCOUNT + "\n"
        return ""

    def json(self, *arguments):
        self.calls.append(arguments)
        if arguments[:2] == ("iam", "get-open-id-connect-provider") and not self.provider_exists:
            raise AwsError("NoSuchEntity")
        if arguments[:2] == ("iam", "create-role") and self.role_exists:
            raise AwsError("An error occurred (EntityAlreadyExists) when calling the CreateRole operation")
        return {}

    def operations(self):
        return [call[1] for call in self.calls]

    def argument(self, operation, name):
        call = next(call for call in self.calls if call[1] == operation)
        return call[call.index(name) + 1]


def run(fake, arguments, deploy_bucket=None):
    """Runs the script with fake as the AWS CLI, and deploy_bucket in ISLAND_BATTLES_DEPLOY_BUCKET."""
    environment = {key: value for key, value in os.environ.items() if key != "ISLAND_BATTLES_DEPLOY_BUCKET"}
    if deploy_bucket is not None:
        environment["ISLAND_BATTLES_DEPLOY_BUCKET"] = deploy_bucket
    output, errors = io.StringIO(), io.StringIO()
    with mock.patch.object(github_apply_role, "aws", fake.text), \
            mock.patch.object(github_apply_role, "aws_json", fake.json), \
            mock.patch.object(deploy_files, "aws", fake.text), \
            mock.patch.dict(os.environ, environment, clear=True), \
            contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
        status = github_apply_role.main(arguments)
    return status, output.getvalue(), errors.getvalue()


class PolicyTest(unittest.TestCase):
    def setUp(self):
        self.policy = github_apply_role.permissions_policy(ACCOUNT, DEPLOY_BUCKET)
        self.statements = {statement["Sid"]: statement for statement in self.policy["Statement"]}

    def test_trusts_only_the_main_only_production_environment(self):
        trust = github_apply_role.trust_policy(ACCOUNT, REPOSITORY)["Statement"]
        self.assertEqual(len(trust), 1)
        self.assertEqual(trust[0]["Principal"], {
            "Federated": f"arn:aws:iam::{ACCOUNT}:oidc-provider/token.actions.githubusercontent.com"})
        self.assertEqual(trust[0]["Condition"], {"StringEquals": {
            "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
            "token.actions.githubusercontent.com:sub": f"repo:{REPOSITORY}:environment:production",
        }})

    def test_manages_only_the_services_with_resources_in_terraform(self):
        self.assertEqual(set(self.statements["ManageTheDeployment"]["Action"]), {
            "lambda:*", "logs:*", "cloudwatch:*", "sns:*", "budgets:*", "acm:*", "cloudfront:*"})
        # The VM is INTODAN/mpl's since the cutover.
        for statement in self.policy["Statement"]:
            self.assertFalse(any(action.lower().startswith("lightsail:") for action in listed(statement["Action"])),
                             statement["Sid"])
        self.assertFalse(allows(self.policy, "lightsail:DeleteInstance", "*"))

    def test_manages_its_roles_and_no_users(self):
        self.assertEqual(self.statements["ManageItsRoles"]["Resource"], [
            f"arn:aws:iam::{ACCOUNT}:role/island-battles-*",
            f"arn:aws:iam::{ACCOUNT}:oidc-provider/token.actions.githubusercontent.com",
        ])
        iam = [statement for statement in self.policy["Statement"]
               if any(action.startswith("iam:") for action in listed(statement["Action"]))]
        self.assertFalse(any(":user/" in resource for statement in iam for resource in listed(statement["Resource"])))
        self.assertFalse(allows(self.policy, "iam:CreateUser", f"arn:aws:iam::{ACCOUNT}:user/island-battles-backups"))
        # deploy/terraform/bot_runners.tf names the runners' role
        # <name>-bot-runner, and the functions' logs are logs:*.
        self.assertTrue(allows(self.policy, "iam:PutRolePolicy",
                               f"arn:aws:iam::{ACCOUNT}:role/island-battles-bot-runner"))
        self.assertTrue({"lambda:*", "logs:*"} <= set(self.statements["ManageTheDeployment"]["Action"]))
        self.assertEqual(self.statements["LeaveThisRoleAlone"], {
            "Sid": "LeaveThisRoleAlone", "Effect": "Deny", "Action": "iam:*",
            "Resource": f"arn:aws:iam::{ACCOUNT}:role/island-battles-github-apply"})
        self.assertFalse(allows(self.policy, "iam:PutRolePolicy",
                                f"arn:aws:iam::{ACCOUNT}:role/island-battles-github-apply"))
        allowed = [statement for statement in self.policy["Statement"] if statement["Effect"] == "Allow"]
        self.assertFalse(any(statement["Resource"] == "*" and "iam:*" in statement["Action"] for statement in allowed),
                         "IAM must stay limited to the deployment's own roles")

    def test_manages_terraforms_buckets_but_not_the_deploy_bucket(self):
        patterns = self.statements["ManageItsBuckets"]["Resource"]
        self.assertEqual(self.statements["ManageItsBuckets"]["Action"], "s3:*")
        for bucket in TERRAFORM_BUCKETS:
            for resource in (f"arn:aws:s3:::{bucket}", f"arn:aws:s3:::{bucket}/some/object"):
                self.assertTrue(any(wildcard_matches(pattern, resource) for pattern in patterns), resource)
                self.assertEqual(allowed_s3_actions(self.policy, resource), set(S3_ACTIONS), resource)
        for resource in (DEPLOY, f"{DEPLOY}/terraform/terraform.tfstate"):
            self.assertFalse(any(wildcard_matches(pattern, resource) for pattern in patterns), resource)

    def test_writes_nothing_of_mpls_in_the_deploy_bucket(self):
        # ManageTheDeployment's "*" is for other services' actions only.
        s3 = [statement for statement in self.policy["Statement"] if statement["Effect"] == "Allow"
              and any(wildcard_matches(action, "s3:GetObject", True) or action.startswith("s3:")
                      for action in listed(statement["Action"]))]
        self.assertNotIn("ManageTheDeployment", [statement["Sid"] for statement in s3])
        for key in MPL_KEYS:
            resource = f"{DEPLOY}/{key}"
            self.assertFalse(any(wildcard_matches(pattern, resource)
                                 for statement in s3 for pattern in listed(statement["Resource"])), key)
            self.assertEqual(allowed_s3_actions(self.policy, resource), set(), key)

    def test_reads_nothing_of_mpls_but_its_tenants_settings(self):
        self.assertEqual(self.statements["KeepOutOfMplsPart"], {
            "Sid": "KeepOutOfMplsPart", "Effect": "Deny", "Action": "s3:GetObject*",
            "Resource": [f"{DEPLOY}/mpl/files/*", f"{DEPLOY}/mpl/terraform/*", f"{DEPLOY}/mpl/plans/*",
                         f"{DEPLOY}/mpl/vm-owner"]})
        self.assertEqual([statement["Sid"] for statement in self.policy["Statement"] if statement["Effect"] == "Deny"],
                         ["KeepOutOfMplsPart", "LeaveThisRoleAlone"])
        role = with_read_only_access(self.policy)
        # With ReadOnlyAccess, whether the role may read each key, in every
        # way: mpl's settings, secrets, state, lock, plans and VM owner marker
        # are denied, in every version the bucket keeps. Island Battles'
        # settings under mpl/tenants/, and the deployment's own keys, aren't.
        expected = {
            "mpl/files/vm.yml": False,
            "mpl/files/lambda.env": False,
            "mpl/files/secrets/postgres_admin_password": False,
            "mpl/files/secrets/tenants/island-battles/postgres_password": False,
            "mpl/terraform/terraform.tfstate": False,
            "mpl/terraform/terraform.tfstate.tflock": False,
            "mpl/plans/123/tfplan": False,
            "mpl/vm-owner": False,
            "mpl/tenants/island-battles/tenant.env": True,
            "terraform/terraform.tfstate": True,
            "terraform/terraform.tfstate.tflock": True,
            "files/aws.tfvars": True,
            "files/secrets/admin_api_key": True,
            "plans/123/tfplan": True,
        }
        for key, readable in expected.items():
            for action in OBJECT_READS:
                self.assertEqual(allows(role, action, f"{DEPLOY}/{key}"), readable, f"{action} {key}")
        # Listing the bucket stays allowed, and so does reading other buckets.
        for action in ("s3:ListBucket", "s3:ListBucketVersions"):
            self.assertTrue(allows(role, action, DEPLOY), action)
        for bucket in TERRAFORM_BUCKETS:
            self.assertTrue(allows(role, "s3:GetObject", f"arn:aws:s3:::{bucket}/mpl/files/x"), bucket)
        # The limit: an IAM Deny can't name every tenant but Island Battles,
        # so another tenant's settings stay readable (#264).
        self.assertTrue(allows(role, "s3:GetObject", f"{DEPLOY}/mpl/tenants/another-game/tenant.env"))

    def test_denies_the_deploy_role_the_same_keys(self):
        terraform = (pathlib.Path(__file__).resolve().parent / "terraform" / "github.tf").read_text()
        keys = re.search(r"^  mpl_unreadable_keys = (\[.*\])$", terraform, re.MULTILINE)
        self.assertIsNotNone(keys, "github.tf must define mpl_unreadable_keys on one line")
        self.assertEqual(json.loads(keys.group(1)), github_apply_role.MPL_UNREADABLE_KEYS)

    def test_matches_like_iam(self):
        # IAM's wildcards span slashes, and [ ] aren't special as in fnmatch.
        self.assertTrue(wildcard_matches("arn:aws:s3:::b/mpl/files/*", "arn:aws:s3:::b/mpl/files/secrets/x"))
        self.assertFalse(wildcard_matches("arn:aws:s3:::b/mpl/files/*", "arn:aws:s3:::b/mpl/filesx"))
        self.assertFalse(wildcard_matches("arn:aws:s3:::b/mpl/vm-owner", "arn:aws:s3:::b/mpl/vm-owner2"))
        self.assertFalse(wildcard_matches("arn:aws:s3:::b/[m]pl", "arn:aws:s3:::b/mpl"))
        self.assertTrue(wildcard_matches("arn:aws:s3:::b/m?l", "arn:aws:s3:::b/mpl"))
        self.assertFalse(wildcard_matches("arn:aws:s3:::B/*", "arn:aws:s3:::b/x"))
        self.assertTrue(wildcard_matches("s3:GetObject*", "S3:GETOBJECTVERSION", True))
        self.assertFalse(wildcard_matches("s3:GetObject*", "s3:PutObject", True))
        # NotResource and NotAction apply to everything they don't name, and a
        # Condition is refused rather than ignored.
        statement = {"Effect": "Deny", "Action": "s3:GetObject", "NotResource": "arn:aws:s3:::b/mpl/tenants/*"}
        self.assertTrue(matches(statement, "s3:GetObject", "arn:aws:s3:::other/x"))
        self.assertFalse(matches(statement, "s3:GetObject", "arn:aws:s3:::b/mpl/tenants/x"))
        statement = {"Effect": "Deny", "NotAction": "s3:List*", "Resource": "*"}
        self.assertTrue(matches(statement, "s3:GetObject", "arn:aws:s3:::b/x"))
        self.assertFalse(matches(statement, "s3:ListBucket", "arn:aws:s3:::b"))
        with self.assertRaises(ValueError):
            matches({"Effect": "Deny", "Action": "s3:*", "Resource": "*",
                     "Condition": {"Bool": {"aws:SecureTransport": "false"}}}, "s3:GetObject", "arn:aws:s3:::b/x")

    def test_uses_only_what_an_apply_uses_in_the_deploy_bucket(self):
        expected = {
            "terraform/terraform.tfstate": {"s3:GetObject", "s3:PutObject"},
            "terraform/terraform.tfstate.tflock": {"s3:GetObject", "s3:PutObject", "s3:DeleteObject"},
            "files/settings/aws.tfvars": {"s3:GetObject", "s3:PutObject"},
            "plans/123/tfplan": {"s3:GetObject", "s3:DeleteObject"},
            "terraform/terraform.tfstate.backup": set(),
            "other/key": set(),
        }
        for key, actions in expected.items():
            self.assertEqual(allowed_s3_actions(self.policy, f"{DEPLOY}/{key}"), actions, key)
        # On the bucket itself it may only list: no policy, versioning,
        # lifecycle or deletion.
        self.assertEqual(allowed_s3_actions(self.policy, DEPLOY), {"s3:ListBucket"})

    def test_names_the_deploy_bucket_it_is_given(self):
        policy = github_apply_role.permissions_policy(ACCOUNT, "another-deploy-bucket")
        self.assertEqual(allowed_s3_actions(policy, "arn:aws:s3:::another-deploy-bucket/files/x"),
                         {"s3:GetObject", "s3:PutObject"})
        self.assertEqual(allowed_s3_actions(policy, f"{DEPLOY}/files/x"), set())

    def test_prints_the_policies_without_aws(self):
        fake = FakeAws()
        status, output, _ = run(fake, ["policies", "--account", ACCOUNT, "--repository", REPOSITORY])
        self.assertEqual(status, 0)
        self.assertEqual(fake.calls, [])
        printed = json.loads(output)
        self.assertEqual(printed["managed"], ["arn:aws:iam::aws:policy/ReadOnlyAccess"])
        self.assertEqual(printed["trust"], github_apply_role.trust_policy(ACCOUNT, REPOSITORY))
        self.assertEqual(printed["permissions"], self.policy)

    def test_prints_the_policies_for_another_deploy_bucket(self):
        fake = FakeAws()
        status, output, _ = run(fake, ["policies", "--account", ACCOUNT, "--repository", REPOSITORY,
                                       "--deploy-bucket", "another-deploy-bucket"])
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(output)["permissions"],
                         github_apply_role.permissions_policy(ACCOUNT, "another-deploy-bucket"))

    def test_refuses_a_deploy_bucket_that_isnt_a_bucket_name(self):
        for name in ("*", "island-battles-*", "Island-Battles", "a", "bucket/key", "a..b", "192.168.1.1", ""):
            fake = FakeAws()
            status, output, _ = run(fake, ["policies", "--account", ACCOUNT, "--repository", REPOSITORY,
                                           "--deploy-bucket", name])
            self.assertEqual(status, 2, name)
            self.assertEqual(output, "", name)


class CreateTest(unittest.TestCase):
    def test_creates_the_role_with_both_policies(self):
        fake = FakeAws()
        status, output, _ = run(fake, ["create", "--repository", REPOSITORY])

        self.assertEqual(status, 0)
        self.assertEqual(output.strip(), f"arn:aws:iam::{ACCOUNT}:role/island-battles-github-apply")
        # The second get-caller-identity names the deploy bucket, as
        # deploy_files.py does.
        self.assertEqual(fake.operations(), [
            "get-caller-identity", "get-caller-identity", "get-open-id-connect-provider", "create-role",
            "attach-role-policy", "put-role-policy"])
        self.assertEqual(json.loads(fake.argument("create-role", "--assume-role-policy-document")),
                         github_apply_role.trust_policy(ACCOUNT, REPOSITORY))
        self.assertEqual(fake.argument("attach-role-policy", "--policy-arn"), "arn:aws:iam::aws:policy/ReadOnlyAccess")
        self.assertEqual(json.loads(fake.argument("put-role-policy", "--policy-document")),
                         github_apply_role.permissions_policy(ACCOUNT, DEPLOY_BUCKET))

    def test_names_the_deploy_bucket_from_the_environment(self):
        fake = FakeAws()
        status, _, errors = run(fake, ["create", "--repository", REPOSITORY], deploy_bucket="another-deploy-bucket")

        self.assertEqual(status, 0)
        self.assertEqual(json.loads(fake.argument("put-role-policy", "--policy-document")),
                         github_apply_role.permissions_policy(ACCOUNT, "another-deploy-bucket"))
        self.assertIn("another-deploy-bucket", errors)

    def test_refuses_a_deploy_bucket_from_the_environment_that_isnt_a_bucket_name(self):
        fake = FakeAws()
        status, _, errors = run(fake, ["create", "--repository", REPOSITORY], deploy_bucket="island-battles-*")

        self.assertEqual(status, 1)
        self.assertNotIn("create-role", fake.operations())
        self.assertNotIn("put-role-policy", fake.operations())
        self.assertIn("ISLAND_BATTLES_DEPLOY_BUCKET", errors)

    def test_takes_the_deploy_bucket_only_from_the_environment(self):
        fake = FakeAws()
        status, _, errors = run(fake, ["create", "--repository", REPOSITORY, "--deploy-bucket", "another"])

        self.assertEqual(status, 2)
        self.assertEqual(fake.calls, [])
        self.assertIn("ISLAND_BATTLES_DEPLOY_BUCKET", errors)

    def test_updates_an_existing_roles_trust_and_policies(self):
        fake = FakeAws(role_exists=True)
        status, _, errors = run(fake, ["create", "--repository", REPOSITORY])

        self.assertEqual(status, 0)
        self.assertIn("update-assume-role-policy", fake.operations())
        self.assertIn("put-role-policy", fake.operations())
        self.assertIn("exists", errors)

    def test_needs_githubs_oidc_provider(self):
        fake = FakeAws(provider_exists=False)
        status, _, errors = run(fake, ["create", "--repository", REPOSITORY])

        self.assertEqual(status, 1)
        self.assertNotIn("create-role", fake.operations())
        self.assertIn("github_repository", errors)

    def test_rejects_a_repository_that_isnt_owner_and_name(self):
        fake = FakeAws()
        status, _, _ = run(fake, ["create", "--repository", "https://github.com/INTODAN/island-battles-go"])
        self.assertEqual(status, 2)
        self.assertEqual(fake.calls, [])


if __name__ == "__main__":
    unittest.main()
