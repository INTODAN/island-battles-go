"""The env_file filter: KEY=value lines, as INTODAN/mpl writes tenant.env, to a dict.

Values are taken literally, without quotes or escapes. Blank lines and lines
starting with # are skipped, and a repeated key keeps its last value, as
Terraform reads lambda.env (deploy/terraform/lambda.tf).
"""

import re

LINE = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$")


def env_file(text):
    settings = {}
    for number, line in enumerate(str(text).splitlines(), start=1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        match = LINE.match(line)
        if not match:
            raise ValueError(f"line {number} isn't KEY=value")
        settings[match.group(1)] = match.group(2)
    return settings


class FilterModule:
    def filters(self):
        return {"env_file": env_file}
