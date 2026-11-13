"""Tests for check_deploy_plan.py with synthetic plans.

Run from the repository root:

    python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import contextlib
import io
import json
import unittest

import check_deploy_plan


def change(address, actions, before_name=None, before=None):
    resource_type = address.split(".")[0]
    if before is None and before_name is not None:
        before = {"name": before_name}
    return {
        "address": address,
        "mode": "managed",
        "type": resource_type,
        "change": {"actions": actions, "before": before},
    }


def run(mode, *changes):
    """Run the script's main on a plan, returning (exit code, stdout, stderr)."""
    plan = {"resource_changes": list(changes)}
    stdout, stderr = io.StringIO(), io.StringIO()
    stdin = io.StringIO(json.dumps(plan))
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        original_stdin = check_deploy_plan.sys.stdin
        check_deploy_plan.sys.stdin = stdin
        try:
            code = check_deploy_plan.main([mode])
        finally:
            check_deploy_plan.sys.stdin = original_stdin
    return code, stdout.getvalue(), stderr.getvalue()


FUNCTION_UPDATE = change("aws_lambda_function.backend", ["update"], "island-battles")
INSTANCE_REPLACE = change('aws_lightsail_instance.vm["island-battles-2"]', ["delete", "create"], "island-battles-2")
INSTANCE_DELETE = change('aws_lightsail_instance.vm["island-battles-1"]', ["delete"], "island-battles-1")
INSTANCE_NO_OP = change('aws_lightsail_instance.vm["island-battles-1"]', ["no-op"], "island-battles-1")
PORTS_UPDATE = change('aws_lightsail_instance_public_ports.vm["island-battles-1"]', ["update"])
ATTACHMENT_REPLACE = change("aws_lightsail_static_ip_attachment.vm", ["delete", "create"])
STATIC_IP_DELETE = change("aws_lightsail_static_ip.vm", ["delete"], "island-battles-vm")
URL_REPLACE = change("aws_lambda_function_url.backend", ["create", "delete"])
LOG_GROUP_CREATE = change("aws_cloudwatch_log_group.backend", ["create"])
CERTIFICATE_CREATE = change("aws_acm_certificate.game", ["create"])
DISTRIBUTION_UPDATE = change("aws_cloudfront_distribution.game", ["update"])
DISTRIBUTION_REPLACE = change("aws_cloudfront_distribution.game", ["delete", "create"],
                              before={"domain_name": "d111111abcdef8.cloudfront.net"})


class DeployModeTest(unittest.TestCase):
    def test_allows_updating_the_function_and_creating_resources(self):
        code, _, stderr = run("deploy", FUNCTION_UPDATE, LOG_GROUP_CREATE, INSTANCE_NO_OP)
        self.assertEqual(code, 0, stderr)

    def test_allows_creating_the_bot_package_bucket_with_the_function_update(self):
        # The plan of #221: the bucket and its settings, the role's policy,
        # and the function's new setting.
        created = [change(address, ["create"]) for address in (
            "aws_s3_bucket.bot_packages", "aws_s3_bucket_public_access_block.bot_packages",
            "aws_s3_bucket_server_side_encryption_configuration.bot_packages",
            "aws_s3_bucket_ownership_controls.bot_packages", "aws_s3_bucket_lifecycle_configuration.bot_packages",
            "aws_s3_bucket_cors_configuration.bot_packages", "aws_s3_bucket_policy.bot_packages",
            "aws_iam_role_policy.lambda_bot_packages")]
        code, _, stderr = run("deploy", FUNCTION_UPDATE, *created)
        self.assertEqual(code, 0, stderr)
        self.assertEqual(run("publishes-backend", FUNCTION_UPDATE, *created)[1], "yes\n")

    def test_refuses_deleting_the_bot_package_bucket(self):
        code, _, stderr = run("deploy", change("aws_s3_bucket.bot_packages", ["delete"], "island-battles-bot-packages"))
        self.assertEqual(code, 1)
        self.assertIn("aws_s3_bucket.bot_packages: delete", stderr)

    def test_refuses_any_lightsail_change(self):
        code, _, stderr = run("deploy", FUNCTION_UPDATE, PORTS_UPDATE)
        self.assertEqual(code, 1)
        self.assertIn("aws_lightsail_instance_public_ports", stderr)

    def test_refuses_creating_the_certificate_and_changing_the_distribution(self):
        code, _, stderr = run("deploy", FUNCTION_UPDATE, CERTIFICATE_CREATE, DISTRIBUTION_UPDATE)
        self.assertEqual(code, 1)
        self.assertIn("aws_acm_certificate.game: create (the game's certificate)", stderr)
        self.assertIn("aws_cloudfront_distribution.game: update (the game's distribution)", stderr)

    def test_refuses_replacing_the_function_url(self):
        code, _, stderr = run("deploy", URL_REPLACE)
        self.assertEqual(code, 1)
        self.assertIn("aws_lambda_function_url.backend: create/delete", stderr)


class ApplyModeTest(unittest.TestCase):
    def test_lists_nothing_without_destruction(self):
        code, stdout, _ = run("apply", FUNCTION_UPDATE, PORTS_UPDATE, ATTACHMENT_REPLACE, INSTANCE_NO_OP)
        self.assertEqual((code, stdout), (0, ""))

    def test_lists_replaced_and_deleted_instances_by_name(self):
        code, stdout, stderr = run("apply", INSTANCE_REPLACE, INSTANCE_DELETE, FUNCTION_UPDATE)
        self.assertEqual(code, 0)
        self.assertEqual(stdout.splitlines(), ["island-battles-2", "island-battles-1"])
        self.assertIn("replaces the instance, with its disk and its database", stderr)
        self.assertIn("deletes the instance", stderr)

    def test_lists_a_deleted_static_ip(self):
        _, stdout, stderr = run("apply", STATIC_IP_DELETE)
        self.assertEqual(stdout.splitlines(), ["island-battles-vm"])
        self.assertIn("static IP", stderr)

    def test_lists_a_replaced_distribution_by_its_domain(self):
        _, stdout, stderr = run("apply", DISTRIBUTION_REPLACE, CERTIFICATE_CREATE)
        self.assertEqual(stdout.splitlines(), ["d111111abcdef8.cloudfront.net"])
        self.assertIn("replaces the distribution", stderr)

    def test_falls_back_to_the_address_without_a_name(self):
        _, stdout, _ = run("apply", change('aws_lightsail_disk.data["x"]', ["delete"]))
        self.assertEqual(stdout.splitlines(), ['aws_lightsail_disk.data["x"]'])


class PublishesBackendModeTest(unittest.TestCase):
    def test_yes_when_the_function_changes(self):
        self.assertEqual(run("publishes-backend", FUNCTION_UPDATE)[1], "yes\n")

    def test_no_when_only_the_static_ip_moves(self):
        self.assertEqual(run("publishes-backend", ATTACHMENT_REPLACE)[1], "no\n")


class UsageTest(unittest.TestCase):
    def test_rejects_an_unknown_mode(self):
        self.assertEqual(run("destroy")[0], 2)


if __name__ == "__main__":
    unittest.main()
