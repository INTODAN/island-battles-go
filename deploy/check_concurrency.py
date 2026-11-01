"""Check aws.tfvars against the account's real Lambda concurrency limit.

scripts/aws.sh runs this before it plans, with one of three modes:

`unreserved <tfvars>` prints `yes` when the settings leave the function
unreserved (`reserved_concurrency = null`), and `no` otherwise. Only an
unreserved function needs the account's limit looked up.

`region <tfvars>` prints the `region` that the settings name, or nothing.

`check <tfvars> <limit>` compares `account_concurrency_limit` with <limit>,
the account's "Concurrent executions" from `aws lambda get-account-settings`.
With no reservation, the account limit is the cap, and Terraform checks the
Postgres connections and the free-tier budget against `account_concurrency_limit`
(deploy/README.md, "The concurrency cap"). A stale, lower value checks the
wrong cap, so `check` refuses (exit 1) when it is below the real limit. It
accepts a reservation, or an `account_concurrency_limit` at or above the limit.

The settings are parsed like Terraform's variable files for the variables read
here: `name = value` lines, comments with #, // or /* */, and the defaults of
deploy/terraform/variables.tf (5 and 10) for a missing variable.
"""

import re
import sys

# Mirror deploy/terraform/variables.tf.
DEFAULT_RESERVED_CONCURRENCY = 5
DEFAULT_ACCOUNT_CONCURRENCY_LIMIT = 10

ASSIGNMENT = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_-]*)\s*=\s*(.*?)\s*$")
NUMBER = re.compile(r"^-?[0-9]+(\.[0-9]+)?$")
MODES = ("unreserved", "region", "check")


class SettingsError(Exception):
    pass


def strip_comment(line):
    """Cut a # or // comment that isn't inside a double-quoted string."""
    in_string = False
    index = 0
    while index < len(line):
        char = line[index]
        if in_string:
            if char == "\\":
                index += 1
            elif char == '"':
                in_string = False
        elif char == '"':
            in_string = True
        elif char == "#" or line.startswith("//", index):
            return line[:index]
        index += 1
    return line


def parse_settings(text):
    """Return {name: raw value} for the file's single-line `name = value` lines.

    That is all these variables ever are. Lines inside a /* */ comment are
    skipped.
    """
    settings = {}
    in_block_comment = False
    for line in text.splitlines():
        if in_block_comment:
            if "*/" not in line:
                continue
            line = line.split("*/", 1)[1]
            in_block_comment = False
        while "/*" in line:
            before, after = line.split("/*", 1)
            if "*/" in after:
                line = before + after.split("*/", 1)[1]
            else:
                line = before
                in_block_comment = True
        match = ASSIGNMENT.match(strip_comment(line))
        if match:
            settings[match.group(1)] = match.group(2)
    return settings


def unquote(raw):
    return raw[1:-1] if len(raw) >= 2 and raw[0] == raw[-1] == '"' else raw


def number_setting(settings, name, default):
    """Return the variable as a number, None for null, or the default if missing."""
    if name not in settings:
        return default
    raw = settings[name]
    if raw == "null":
        return None
    # Terraform converts a quoted number to a number.
    text = unquote(raw)
    if not NUMBER.match(text):
        raise SettingsError(f"{name} = {raw} isn't a number or null.")
    value = float(text)
    return int(value) if value == int(value) else value


def reserved_concurrency(settings):
    return number_setting(settings, "reserved_concurrency", DEFAULT_RESERVED_CONCURRENCY)


def account_concurrency_limit(settings):
    value = number_setting(settings, "account_concurrency_limit", DEFAULT_ACCOUNT_CONCURRENCY_LIMIT)
    if value is None:
        raise SettingsError("account_concurrency_limit = null isn't a number.")
    return value


def region(settings):
    return unquote(settings.get("region", ""))


def stale_limit_message(configured, real):
    return (
        f"reserved_concurrency is null, so the account's concurrency limit is the Lambda's cap, but "
        f"account_concurrency_limit is {configured} in aws.tfvars and the account's limit is {real}.\n"
        f"Terraform would check the Postgres connections and the free-tier budget against {configured} "
        f"instead of {real}. Either set reserved_concurrency again, for example back to 5, or set "
        f"account_concurrency_limit = {real} and check the budget it implies.\n"
        'See "The concurrency cap" in deploy/README.md.'
    )


def check(settings, real_limit):
    """Return the refusal message, or None when the settings are fine."""
    if reserved_concurrency(settings) is not None:
        return None
    configured = account_concurrency_limit(settings)
    if configured < real_limit:
        return stale_limit_message(configured, real_limit)
    return None


def read_settings(path):
    try:
        with open(path, encoding="utf-8") as file:
            return parse_settings(file.read())
    except OSError as error:
        raise SettingsError(f"Can't read {path}: {error.strerror}.") from error


def main(arguments):
    if not arguments or arguments[0] not in MODES or len(arguments) != (3 if arguments[0] == "check" else 2):
        print("Usage: check_concurrency.py unreserved|region <tfvars> | check <tfvars> <limit>", file=sys.stderr)
        return 2
    mode = arguments[0]
    try:
        settings = read_settings(arguments[1])
        if mode == "region":
            print(region(settings))
        elif mode == "unreserved":
            print("yes" if reserved_concurrency(settings) is None else "no")
        else:
            limit = arguments[2].strip()
            if not limit.isdigit():
                raise SettingsError(f"The account's concurrency limit, {limit!r}, isn't a number.")
            message = check(settings, int(limit))
            if message:
                print(message, file=sys.stderr)
                return 1
    except SettingsError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
