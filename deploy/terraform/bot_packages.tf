# Bot packages (#221): the zips that members upload to a lobby, built by the
# SDKs' package command. A browser or the CLI uploads a zip straight here with
# a presigned POST that the backend hands out, because a Function URL takes at
# most 6 MB. The backend reads each zip's central directory and manifest with
# range requests, and deletes the zips it replaces or refuses. Keys are
# lobbies/<lobby_id>/<package_id>.zip. See deploy/README.md, "Bot packages".

locals {
  bot_packages_bucket = "${var.name}-bot-packages"
  # The origins that upload from a browser: the game's 3D client at /play/
  # and the lobby page at /lobby/ui/, which are both on game_hostname.
  bot_package_upload_origins = distinct([
    "https://${var.game_hostname}", # the game
    "https://${var.game_hostname}", # the lobby page
  ])
}

resource "aws_s3_bucket" "bot_packages" {
  bucket = local.bot_packages_bucket
}

resource "aws_s3_bucket_public_access_block" "bot_packages" {
  bucket                  = aws_s3_bucket.bot_packages.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# SSE-S3. Versioning stays off: a replaced or deleted package is gone.
resource "aws_s3_bucket_server_side_encryption_configuration" "bot_packages" {
  bucket = aws_s3_bucket.bot_packages.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_ownership_controls" "bot_packages" {
  bucket = aws_s3_bucket.bot_packages.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# Every zip is deleted 8 days after its upload. The backend's game retention
# deletes the packages' rows as long after, so neither outlives the other.
resource "aws_s3_bucket_lifecycle_configuration" "bot_packages" {
  bucket = aws_s3_bucket.bot_packages.id

  rule {
    id     = "expire-bot-packages"
    status = "Enabled"

    filter {}

    expiration {
      days = 8
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

# Browsers POST the zip from the game's pages. A form POST with an upload
# progress listener is preflighted, so the preflight is answered too.
resource "aws_s3_bucket_cors_configuration" "bot_packages" {
  bucket = aws_s3_bucket.bot_packages.id

  cors_rule {
    allowed_methods = ["POST"]
    allowed_origins = local.bot_package_upload_origins
    allowed_headers = ["*"]
    max_age_seconds = 3000
  }
}

resource "aws_s3_bucket_policy" "bot_packages" {
  bucket = aws_s3_bucket.bot_packages.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "RequireTls"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.bot_packages.arn, "${aws_s3_bucket.bot_packages.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.bot_packages]
}

# The backend signs the presigned POSTs with its own role, so the role must
# be allowed to put the objects it signs for. It reads a zip's directory and
# manifest, and deletes the zips that a new package replaces, that fail their
# check, or that their owner deletes. This bucket only.
resource "aws_iam_role_policy" "lambda_bot_packages" {
  name = "bot-packages"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "BotPackages"
      Effect   = "Allow"
      Action   = ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]
      Resource = "${aws_s3_bucket.bot_packages.arn}/*"
    }]
  })
}
