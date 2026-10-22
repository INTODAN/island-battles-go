"""Compare JSON values produced by different languages."""

import math


def assert_matches(actual, expected, path="$"):
    """Compare JSON values, allowing float rounding."""
    if isinstance(expected, bool) or isinstance(actual, bool):
        assert actual is expected, f"{path}: {actual!r} != {expected!r}"
    elif isinstance(expected, (int, float)):
        assert isinstance(actual, (int, float)), f"{path}: {actual!r} is not a number"
        assert math.isclose(actual, expected, rel_tol=1e-9, abs_tol=1e-12), (
            f"{path}: {actual} != {expected}"
        )
    elif isinstance(expected, dict):
        assert isinstance(actual, dict) and actual.keys() == expected.keys(), (
            f"{path}: keys {sorted(actual)} != {sorted(expected)}"
        )
        for key in expected:
            assert_matches(actual[key], expected[key], f"{path}.{key}")
    elif isinstance(expected, list):
        assert isinstance(actual, list) and len(actual) == len(expected), (
            f"{path}: {actual!r} != {expected!r}"
        )
        for index, (left, right) in enumerate(zip(actual, expected)):
            assert_matches(left, right, f"{path}[{index}]")
    else:
        assert actual == expected, f"{path}: {actual!r} != {expected!r}"
