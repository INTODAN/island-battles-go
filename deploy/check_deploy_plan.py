"""Inspect the Terraform plan that scripts/aws.sh is about to apply.

scripts/aws.sh pipes `terraform show -json <plan>` into this script, with one
of three modes:

`deploy` refuses a plan that a backend deploy must not apply. `scripts/aws.sh
deploy` applies its plan without asking, so it allows only creating or
updating resources other than Lightsail's, ACM's and CloudFront's. A deploy
then never touches the VM, or the game's certificate and distribution, and
never deletes or replaces anything, such as the Function URL that CloudFront
calls. Creating the certificate would wait for a DNS record that only a person
can add. Anything else goes through `scripts/aws.sh apply`, which shows the
plan and asks.

`apply` refuses, with exit status 1, a plan that destroys anything
INTODAN/mpl owns since the cutover (#232): a Lightsail resource of the VM,
which this configuration removes from its state with `removed` blocks and
never destroys, or the account's GitHub OIDC provider, which INTODAN/mpl's
roles trust too. Nothing allows that: such a plan is a mistake in the
configuration or in aws.tfvars. Otherwise it prints, one per line, the name
of every CloudFront distribution the plan deletes or replaces, and describes
them on stderr: a new distribution has a new domain for the game's CNAME.
`scripts/aws.sh apply` then asks for each name to be typed, and the Deploy
workflow's apply needs each one in allow_destroy.

`publishes-backend` prints `yes` when the plan creates or updates the backend
function, so `scripts/aws.sh apply` runs the migrations first, and `no`
otherwise.
"""

import json
import sys

ALLOWED_ACTIONS = (["create"], ["update"])
BACKEND_FUNCTION = "aws_lambda_function.backend"
# A deploy changes none of these: the VM and the game's name.
REFUSED_TYPE_PREFIXES = {
    "aws_lightsail_": "a Lightsail resource, which INTODAN/mpl owns",
    "aws_acm_": "the game's certificate",
    "aws_cloudfront_": "the game's distribution",
}
# INTODAN/mpl owns these since the cutover (#232); no plan here may destroy
# them.
MPL_OWNED_TYPE_PREFIXES = {
    "aws_lightsail_": "the VM's Lightsail resources, which INTODAN/mpl owns",
    "aws_iam_openid_connect_provider": "the account's GitHub OIDC provider, which INTODAN/mpl's roles trust too",
}
# Deleting one of these loses an address that DNS points at.
DESTRUCTIVE_TYPES = {
    "aws_cloudfront_distribution": "distribution, whose domain the game's CNAME points at",
}
MODES = ("deploy", "apply", "publishes-backend")


def managed_changes(plan):
    """Yield every managed resource change in the plan that does something."""
    for change in plan.get("resource_changes", []):
        if change.get("mode") != "managed":
            continue
        if change["change"]["actions"] == ["no-op"]:
            continue
        yield change


def refused_changes(plan):
    """Return a description of every change in the plan a deploy may not make."""
    refused = []
    for change in managed_changes(plan):
        actions = change["change"]["actions"]
        address = change["address"]
        kind = next((kind for prefix, kind in REFUSED_TYPE_PREFIXES.items() if change["type"].startswith(prefix)), None)
        if kind is not None:
            refused.append(f"{address}: {'/'.join(actions)} ({kind})")
        elif actions not in ALLOWED_ACTIONS:
            refused.append(f"{address}: {'/'.join(actions)}")
    return refused


def mpl_owned_destroys(plan):
    """Return a description of every change that destroys what INTODAN/mpl owns."""
    refused = []
    for change in managed_changes(plan):
        actions = change["change"]["actions"]
        kind = next((kind for prefix, kind in MPL_OWNED_TYPE_PREFIXES.items() if change["type"].startswith(prefix)), None)
        if kind is not None and "delete" in actions:
            refused.append(f"{change['address']}: {'/'.join(actions)} ({kind})")
    return refused


def destroyed_resources(plan):
    """Return (name, description) for every destructive deletion or replacement."""
    destroyed = []
    for change in managed_changes(plan):
        kind = DESTRUCTIVE_TYPES.get(change["type"])
        actions = change["change"]["actions"]
        if kind is None or "delete" not in actions:
            continue
        before = change["change"].get("before") or {}
        # A distribution has no name; its domain is what the CNAME holds.
        name = before.get("name") or before.get("domain_name") or change["address"]
        verb = "deletes" if actions == ["delete"] else "replaces"
        destroyed.append((name, f"{change['address']}: {verb} the {kind}"))
    return destroyed


def publishes_backend(plan):
    """Whether the plan creates the backend function or changes it."""
    return any(change["address"] == BACKEND_FUNCTION for change in managed_changes(plan))


def main(arguments):
    if len(arguments) != 1 or arguments[0] not in MODES:
        print(f"Usage: terraform show -json <plan> | check_deploy_plan.py {'|'.join(MODES)}", file=sys.stderr)
        return 2
    mode = arguments[0]
    plan = json.load(sys.stdin)
    if mode == "publishes-backend":
        print("yes" if publishes_backend(plan) else "no")
        return 0
    if mode == "apply":
        refused = mpl_owned_destroys(plan)
        if refused:
            print("This plan destroys what INTODAN/mpl owns, so nothing is applied:", file=sys.stderr)
            for line in refused:
                print(f"  {line}", file=sys.stderr)
            print("Take Lightsail resources out of the state with a removed block (lifecycle { destroy = false }), "
                  "and keep github_repository set, so the OIDC provider stays (deploy/README.md, \"The cutover\").",
                  file=sys.stderr)
            return 1
        destroyed = destroyed_resources(plan)
        if destroyed:
            print("This plan destroys resources that are hard to get back:", file=sys.stderr)
            for name, description in destroyed:
                print(f"  {description}", file=sys.stderr)
                print(name)
        return 0
    refused = refused_changes(plan)
    if not refused:
        return 0
    print("This plan changes more than the backend, so the deploy stops before migrating:", file=sys.stderr)
    for line in refused:
        print(f"  {line}", file=sys.stderr)
    print("Review and apply these changes with scripts/aws.sh apply, then deploy again.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
