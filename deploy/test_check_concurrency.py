"""Tests for check_concurrency.py with synthetic aws.tfvars files.

Run from the repository root:

    python3 -I -m unittest discover -s deploy -p 'test_*.py'
"""

import contextlib
import io
import os
import tempfile
import unittest

import check_concurrency


def run(mode, tfvars, *extra):
    """Run main on a tfvars text, returning (exit code, stdout, stderr)."""
    stdout, stderr = io.StringIO(), io.StringIO()
    with tempfile.TemporaryDirectory() as directory:
        path = os.path.join(directory, "aws.tfvars")
        with open(path, "w", encoding="utf-8") as file:
            file.write(tfvars)
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            code = check_concurrency.main([mode, path, *extra])
    return code, stdout.getvalue(), stderr.getvalue()


def parsed(tfvars):
    return check_concurrency.parse_settings(tfvars)


class ParseTest(unittest.TestCase):
    def test_null_number_and_missing(self):
        self.assertIsNone(check_concurrency.reserved_concurrency(parsed("reserved_concurrency = null\n")))
        self.assertEqual(check_concurrency.reserved_concurrency(parsed("reserved_concurrency = 7\n")), 7)
        self.assertEqual(check_concurrency.reserved_concurrency(parsed('region = "eu-central-1"\n')), 5)
        self.assertEqual(check_concurrency.account_concurrency_limit(parsed("")), 10)
        self.assertEqual(check_concurrency.account_concurrency_limit(parsed("account_concurrency_limit = 400\n")), 400)

    def test_spacing_quotes_and_trailing_comments(self):
        self.assertIsNone(check_concurrency.reserved_concurrency(parsed("  reserved_concurrency=null   # none\n")))
        self.assertEqual(check_concurrency.reserved_concurrency(parsed("reserved_concurrency\t=\t3 // three\n")), 3)
        self.assertEqual(check_concurrency.reserved_concurrency(parsed('reserved_concurrency = "4"\n')), 4)
        self.assertEqual(check_concurrency.reserved_concurrency(parsed("reserved_concurrency = 6\r\n")), 6)

    def test_commented_out_lines_are_ignored(self):
        text = "# reserved_concurrency = null\n// reserved_concurrency = 1\n/* reserved_concurrency = 2 */\n"
        self.assertEqual(check_concurrency.reserved_concurrency(parsed(text)), 5)
        block = "/*\nreserved_concurrency = null\n*/\nreserved_concurrency = 8\n"
        self.assertEqual(check_concurrency.reserved_concurrency(parsed(block)), 8)

    def test_the_example_files_commented_settings_do_not_count(self):
        text = "# Set it to null if too low.\n# reserved_concurrency      = 5\n# account_concurrency_limit = 10\n"
        self.assertEqual(parsed(text), {})

    def test_hash_inside_a_string_is_not_a_comment(self):
        self.assertEqual(check_concurrency.region(parsed('region = "eu-central-1" # here\n')), "eu-central-1")
        self.assertEqual(parsed('name = "a#b" # c\n'), {"name": '"a#b"'})

    def test_a_similar_variable_name_does_not_match(self):
        self.assertEqual(check_concurrency.reserved_concurrency(parsed("x_reserved_concurrency = null\n")), 5)

    def test_rejects_a_value_that_is_not_a_number(self):
        with self.assertRaises(check_concurrency.SettingsError):
            check_concurrency.reserved_concurrency(parsed("reserved_concurrency = var.five\n"))
        with self.assertRaises(check_concurrency.SettingsError):
            check_concurrency.account_concurrency_limit(parsed("account_concurrency_limit = null\n"))


class CheckTest(unittest.TestCase):
    def test_refuses_a_stale_limit_when_unreserved(self):
        code, _, stderr = run("check", "reserved_concurrency = null\naccount_concurrency_limit = 10\n", "400")
        self.assertEqual(code, 1)
        self.assertIn("account_concurrency_limit is 10", stderr)
        self.assertIn("limit is 400", stderr)
        self.assertIn("The concurrency cap", stderr)

    def test_refuses_when_the_limit_is_missing_and_defaults_to_10(self):
        code, _, stderr = run("check", "reserved_concurrency = null\n", "400")
        self.assertEqual(code, 1)
        self.assertIn("account_concurrency_limit is 10", stderr)

    def test_accepts_a_matching_limit(self):
        self.assertEqual(run("check", "reserved_concurrency = null\naccount_concurrency_limit = 400\n", "400")[0], 0)
        self.assertEqual(run("check", "reserved_concurrency = null\naccount_concurrency_limit = 10\n", "10")[0], 0)

    def test_accepts_any_reservation_whatever_the_limit(self):
        self.assertEqual(run("check", "reserved_concurrency = 5\naccount_concurrency_limit = 10\n", "400")[0], 0)
        self.assertEqual(run("check", "", "400")[0], 0)

    def test_a_commented_out_null_is_a_reservation(self):
        code, _, _ = run("check", "# reserved_concurrency = null\naccount_concurrency_limit = 10\n", "400")
        self.assertEqual(code, 0)

    def test_rejects_a_limit_that_is_not_a_number(self):
        for limit in ("None", "", "abc"):
            code, _, stderr = run("check", "reserved_concurrency = null\n", limit)
            self.assertEqual(code, 1, limit)
            self.assertIn("isn't a number", stderr)

    def test_a_missing_file_is_an_error(self):
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            code = check_concurrency.main(["check", "/nonexistent/aws.tfvars", "400"])
        self.assertEqual(code, 1)
        self.assertIn("Can't read", stderr.getvalue())


class ModesTest(unittest.TestCase):
    def test_unreserved(self):
        self.assertEqual(run("unreserved", "reserved_concurrency = null\n"), (0, "yes\n", ""))
        self.assertEqual(run("unreserved", "reserved_concurrency = 5\n"), (0, "no\n", ""))
        self.assertEqual(run("unreserved", "")[1], "no\n")

    def test_region(self):
        self.assertEqual(run("region", 'region = "eu-central-1"\n'), (0, "eu-central-1\n", ""))
        self.assertEqual(run("region", "")[1], "\n")

    def test_usage(self):
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            self.assertEqual(check_concurrency.main([]), 2)
            self.assertEqual(check_concurrency.main(["check", "file"]), 2)
            self.assertEqual(check_concurrency.main(["other", "file"]), 2)


if __name__ == "__main__":
    unittest.main()
