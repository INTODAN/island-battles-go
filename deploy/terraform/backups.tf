# The bucket of Island Battles' old database backups (deploy/README.md,
# "Backups"). Until the cutover (#232) the VM dumped Postgres every day and
# uploaded the dump here, with an upload-only key. Since INTODAN/mpl owns the
# VM, its own deploy backs up every database there, Island Battles' included,
# to its own bucket, and stopped this repository's backup. The dumps here
# expire after database_backup_retention_days; the bucket stays until someone
# deletes it on purpose (prevent_destroy).
#
# New buckets encrypt objects (SSE-S3) and disable ACLs on their own.

locals {
  backup_bucket = "${var.name}-backups-${data.aws_caller_identity.current.account_id}"
  # This repository's GitHub roles, when it has them, and the ones named, such
  # as INTODAN/mpl's.
  backup_denied_roles = distinct(concat(
    local.github_deploy ? ["${var.name}-github-deploy", "${var.name}-github-apply"] : [],
    var.backup_denied_roles,
  ))
}

resource "aws_s3_bucket" "backups" {
  bucket = local.backup_bucket

  lifecycle {
    # Terraform can't delete a bucket that has objects anyway; this stops the
    # plan before it tries.
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_public_access_block" "backups" {
  bucket                  = aws_s3_bucket.backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id

  versioning_configuration {
    status = "Enabled"
  }
}

# Each dump is kept for database_backup_retention_days. A dump that something
# overwrote or deleted is kept as a noncurrent version for as long again.
resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id

  rule {
    id     = "expire-old-backups"
    status = "Enabled"

    filter {}

    expiration {
      days = var.database_backup_retention_days
    }

    noncurrent_version_expiration {
      noncurrent_days = var.database_backup_retention_days
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }

  depends_on = [aws_s3_bucket_versioning.backups]
}

# Dumps hold player data. The bucket refuses plain HTTP, and keeps them from
# the GitHub roles, whose ReadOnlyAccess would otherwise let a workflow run
# download them: the deploy role (github.tf) and the apply role
# (deploy/github_apply_role.py), and INTODAN/mpl's two, which share the
# account (var.backup_denied_roles). A condition on the caller's ARN, unlike a
# principal, may name a role that doesn't exist yet.
resource "aws_s3_bucket_policy" "backups" {
  bucket = aws_s3_bucket.backups.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(
      [{
        Sid       = "RequireTls"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [aws_s3_bucket.backups.arn, "${aws_s3_bucket.backups.arn}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      }],
      length(local.backup_denied_roles) > 0 ? [{
        Sid       = "KeepDumpsFromGitHubRoles"
        Effect    = "Deny"
        Principal = "*"
        Action    = ["s3:GetObject", "s3:GetObjectVersion"]
        Resource  = "${aws_s3_bucket.backups.arn}/*"
        Condition = {
          ArnEquals = {
            "aws:PrincipalArn" = [for role in local.backup_denied_roles : "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${role}"]
          }
        }
      }] : [],
    )
  })

  depends_on = [aws_s3_bucket_public_access_block.backups]
}
