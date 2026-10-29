"""Inspect the Terraform plan that scripts/aws.sh is about to apply.

scripts/aws.sh pipes `terraform show -json <plan>` into this script, with one
of three modes:

`deploy` refuses a plan that a backend deploy must not apply. `scripts/aws.sh
deploy` applies its plan without asking, so it allows only creating or
updating resources other than Lightsail's. A deploy then never touches the VM,
its firewall or its static IP, and never deletes or replaces anything, such as
the Function URL that Centrifugo calls. Anything else goes through
`scripts/aws.sh apply`, which shows the plan and asks.

`apply` prints, one per line, the name of every Lightsail instance, static IP
or disk the plan deletes or replaces, and describes them on stderr.
`scripts/aws.sh apply` then asks for each name to be typed, because deleting an
instance deletes its disk and its automatic snapshots.

`publishes-backend` prints `yes` when the plan creates or updates the backend
function, so `scripts/aws.sh apply` runs the migrations first, and `no`
otherwise. An apply that only moves the static IP, for example, must not
migrate: in the middle of a move, vm.<domain> still points at the old VM,
which refuses the backend's logins.
"""

import json
import sys

ALLOWED_ACTIONS = (["create"], ["update"])
BACKEND_FUNCTION = "aws_lambda_function.backend"
# Deleting one of these loses data or the address vm.<domain> points at.
DESTRUCTIVE_TYPES = {
    "aws_lightsail_instance": "instance, with its disk and automatic snapshots",
    "aws_lightsail_static_ip": "static IP, which vm.<domain> points at",
    "aws_lightsail_disk": "disk",
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
        if change["type"].startswith("aws_lightsail_"):
            refused.append(f"{address}: {'/'.join(actions)} (a Lightsail resource)")
        elif actions not in ALLOWED_ACTIONS:
            refused.append(f"{address}: {'/'.join(actions)}")
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
        name = before.get("name") or change["address"]
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
        destroyed = destroyed_resources(plan)
        if destroyed:
            print("This plan destroys Lightsail resources:", file=sys.stderr)
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
