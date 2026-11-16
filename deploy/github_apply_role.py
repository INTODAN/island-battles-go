"""Create the role that lets GitHub Actions apply Terraform from main.

The Deploy workflow's `apply` target applies a plan that its `plan` target
saved. Its jobs run in the `production` environment, which the repository's
settings limit to main, and this role trusts only that environment's jobs,
through GitHub's OIDC provider. So only merged code ever gets it: merging the
pull request is the approval. That is what lets an agent in the cloud deploy
(deploy/README.md, "Deploying from GitHub Actions").

The role reads the whole account (ReadOnlyAccess), as a plan must, and changes
only what deploy/terraform/ manages: Lambda, CloudWatch logs and alarms, SNS,
Budgets, ACM and CloudFront, Terraform's three buckets (the backups, the web
client and the bot packages), IAM roles named island-battles-* and GitHub's
OIDC provider. The VM is INTODAN/mpl's since the cutover (#232), so the role
may change no Lightsail resource, and no IAM user is left for it to manage. It
can't change itself. Because it can write IAM policies for island-battles-*
roles, treat it as close to administrator of the account: reviewing what
merges to main is the control.

INTODAN/mpl shares the deploy bucket under mpl/. There the role may list the
bucket and use only the keys that an apply uses: Terraform's state and its
lock, deploy/local/'s files under files/, and the saved plans under plans/. It
writes nothing under mpl/, and can't change the bucket itself, which
deploy/deploy_files.py's bootstrap manages.

A new bucket in deploy/terraform/ needs its name in MANAGED_BUCKETS, which
ManageItsBuckets allows, and a new run of this script before the apply that
creates it.

It lives outside Terraform, like the deploy bucket, so a Terraform run can't
widen the role that applies it. Run this again after changing it here.

Usage, from the repository root, with credentials that can manage IAM:

    python3 -I deploy/github_apply_role.py create --repository <owner>/<name>
    python3 -I deploy/github_apply_role.py policies --account <id> --repository <owner>/<name> [--deploy-bucket <name>]

`create` names the deploy bucket as deploy/deploy_files.py does:
island-battles-deploy-<account ID>, unless ISLAND_BATTLES_DEPLOY_BUCKET names
another. `policies` prints the trust and permission policies without calling
AWS, for the deploy bucket that --deploy-bucket names, or the default one.
"""

import json
import pathlib
import re
import sys

# `python3 -I` leaves the script's directory off the path.
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from deploy_files import (  # noqa: E402
    BUCKET_ENVIRONMENT_VARIABLE, FILES_PREFIX, PLANS_PREFIX, STATE_KEY, AwsError, aws, aws_json, bucket_name,
    default_bucket_name)

ROLE_NAME = "island-battles-github-apply"
# The deploy role (deploy/terraform/github.tf) trusts the same environment.
ENVIRONMENT = "production"
NAME_PREFIX = "island-battles-"
OIDC_HOST = "token.actions.githubusercontent.com"
READ_POLICY_ARN = "arn:aws:iam::aws:policy/ReadOnlyAccess"
PERMISSIONS_POLICY_NAME = "apply-the-deployment"
# What deploy/terraform/ creates or changes, besides S3 and IAM.
MANAGED_SERVICES = ["lambda", "logs", "cloudwatch", "sns", "budgets", "acm", "cloudfront"]
# The buckets that deploy/terraform/ creates (backups.tf, web_client.tf and
# bot_packages.tf). Each pattern matches the bucket's objects too.
MANAGED_BUCKETS = [f"{NAME_PREFIX}backups-*", f"{NAME_PREFIX}web-client-*", f"{NAME_PREFIX}bot-packages*"]
# The S3 backend's lock file (deploy/terraform/versions.tf, use_lockfile).
LOCK_KEY = f"{STATE_KEY}.tflock"


def oidc_provider_arn(account):
    return f"arn:aws:iam::{account}:oidc-provider/{OIDC_HOST}"


def role_arn(account):
    return f"arn:aws:iam::{account}:role/{ROLE_NAME}"


def valid_bucket_name(name):
    """Whether S3 would take name as a general purpose bucket's name."""
    return (re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", name) is not None
            and ".." not in name and not re.fullmatch(r"[0-9.]+", name))


def trust_policy(account, repository):
    return {
        "Version": "2012-10-17",
        "Statement": [{
            "Effect": "Allow",
            "Principal": {"Federated": oidc_provider_arn(account)},
            "Action": "sts:AssumeRoleWithWebIdentity",
            "Condition": {"StringEquals": {
                f"{OIDC_HOST}:aud": "sts.amazonaws.com",
                f"{OIDC_HOST}:sub": f"repo:{repository}:environment:{ENVIRONMENT}",
            }},
        }],
    }


def permissions_policy(account, deploy_bucket):
    iam = f"arn:aws:iam::{account}"
    deploy = f"arn:aws:s3:::{deploy_bucket}"
    return {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "ManageTheDeployment",
                "Effect": "Allow",
                "Action": [f"{service}:*" for service in MANAGED_SERVICES],
                "Resource": "*",
            },
            {
                "Sid": "ManageItsBuckets",
                "Effect": "Allow",
                "Action": "s3:*",
                "Resource": [f"arn:aws:s3:::{bucket}" for bucket in MANAGED_BUCKETS],
            },
            # The deploy bucket, which INTODAN/mpl shares under mpl/: only what
            # the Deploy workflow's apply target and `scripts/aws.sh
            # apply-plan` use, and nothing on the bucket itself.
            {
                "Sid": "ListTheDeployBucket",
                "Effect": "Allow",
                "Action": "s3:ListBucket",
                "Resource": deploy,
            },
            {
                # Terraform's S3 backend, and deploy_files.py's pull and push.
                "Sid": "UseItsPartOfTheDeployBucket",
                "Effect": "Allow",
                "Action": ["s3:GetObject", "s3:PutObject"],
                "Resource": [f"{deploy}/{STATE_KEY}", f"{deploy}/{LOCK_KEY}", f"{deploy}/{FILES_PREFIX}*"],
            },
            {
                # The workflow's steps "Fetch the plan" and "Delete the saved plan".
                "Sid": "UseTheSavedPlans",
                "Effect": "Allow",
                "Action": ["s3:GetObject", "s3:DeleteObject"],
                "Resource": f"{deploy}/{PLANS_PREFIX}*",
            },
            {
                "Sid": "ReleaseTheStateLock",
                "Effect": "Allow",
                "Action": "s3:DeleteObject",
                "Resource": f"{deploy}/{LOCK_KEY}",
            },
            {
                # deploy/terraform/github.tf creates GitHub's OIDC provider.
                "Sid": "ManageItsRoles",
                "Effect": "Allow",
                "Action": "iam:*",
                "Resource": [f"{iam}:role/{NAME_PREFIX}*", oidc_provider_arn(account)],
            },
            {
                "Sid": "LeaveThisRoleAlone",
                "Effect": "Deny",
                "Action": "iam:*",
                "Resource": role_arn(account),
            },
        ],
    }


def create(repository):
    account = aws("sts", "get-caller-identity", "--query", "Account", "--output", "text").strip()
    deploy_bucket = bucket_name()
    if not valid_bucket_name(deploy_bucket):
        raise AwsError(f"{deploy_bucket!r} isn't a bucket name. Check {BUCKET_ENVIRONMENT_VARIABLE}.")
    try:
        aws_json("iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", oidc_provider_arn(account))
    except AwsError as error:
        raise AwsError(f"{error}\nGitHub's OIDC provider is missing. Set github_repository in "
                       "deploy/local/aws.tfvars and run scripts/aws.sh apply first.") from error
    trust = json.dumps(trust_policy(account, repository))
    try:
        aws_json("iam", "create-role", "--role-name", ROLE_NAME, "--assume-role-policy-document", trust,
                 "--max-session-duration", "3600",
                 "--description", f"GitHub Actions jobs in {repository}'s {ENVIRONMENT} environment apply Terraform",
                 "--tags", "Key=Project,Value=island-battles", "Key=ManagedBy,Value=github_apply_role.py")
        print(f"Created the role {ROLE_NAME}.", file=sys.stderr)
    except AwsError as error:
        if "EntityAlreadyExists" not in str(error):
            raise
        aws("iam", "update-assume-role-policy", "--role-name", ROLE_NAME, "--policy-document", trust)
        print(f"{ROLE_NAME} exists. Updating its policies.", file=sys.stderr)
    aws("iam", "attach-role-policy", "--role-name", ROLE_NAME, "--policy-arn", READ_POLICY_ARN)
    aws("iam", "put-role-policy", "--role-name", ROLE_NAME, "--policy-name", PERMISSIONS_POLICY_NAME,
        "--policy-document", json.dumps(permissions_policy(account, deploy_bucket)))
    print(f"Its policy names the deploy bucket {deploy_bucket}.", file=sys.stderr)
    return role_arn(account)


def main(arguments):
    usage = ("Usage: github_apply_role.py create --repository <owner>/<name>\n"
             "       github_apply_role.py policies --account <id> --repository <owner>/<name> "
             "[--deploy-bucket <name>]")
    if not arguments or arguments[0] not in ("create", "policies"):
        print(usage, file=sys.stderr)
        return 2
    options = dict(zip(arguments[1::2], arguments[2::2]))
    repository = options.get("--repository", "")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        print(usage, file=sys.stderr)
        return 2
    if arguments[0] == "policies":
        account = options.get("--account", "")
        if not re.fullmatch(r"\d{12}", account):
            print(usage, file=sys.stderr)
            return 2
        deploy_bucket = options.get("--deploy-bucket", default_bucket_name(account))
        if not valid_bucket_name(deploy_bucket):
            print(f"{deploy_bucket!r} isn't a bucket name.\n{usage}", file=sys.stderr)
            return 2
        print(json.dumps({"trust": trust_policy(account, repository),
                          "permissions": permissions_policy(account, deploy_bucket),
                          "managed": [READ_POLICY_ARN]}, indent=2))
        return 0
    if "--deploy-bucket" in options:
        print(f"create takes the deploy bucket from {BUCKET_ENVIRONMENT_VARIABLE}, as the scripts do.\n{usage}",
              file=sys.stderr)
        return 2
    try:
        arn = create(repository)
    except AwsError as error:
        print(error, file=sys.stderr)
        return 1
    print(arn)
    print(f"Only jobs in the {ENVIRONMENT} environment can assume it. Keep that environment limited to main "
          "(deploy/README.md, \"Deploying from GitHub Actions\").", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
