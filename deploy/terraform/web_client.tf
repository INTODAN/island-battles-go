# The 3D web client (#104): the built client in clients/island-battles-web/ is
# served at https://<game_hostname>/play/ from a private S3 bucket through the
# game's own distribution, so its API calls stay same-origin and CloudFront
# caches it without the Lambda running. scripts/deploy-web-client.sh uploads a
# build (deploy/README.md, "The web client").

locals {
  web_client_bucket = "${var.name}-web-client-${data.aws_caller_identity.current.account_id}"
}

# Versioning stays off: a rollback re-uploads an earlier build.
resource "aws_s3_bucket" "web_client" {
  bucket = local.web_client_bucket
}

resource "aws_s3_bucket_public_access_block" "web_client" {
  bucket                  = aws_s3_bucket.web_client.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "web_client" {
  bucket = aws_s3_bucket.web_client.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_ownership_controls" "web_client" {
  bucket = aws_s3_bucket.web_client.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_cloudfront_origin_access_control" "web_client" {
  name                              = "${var.name}-web-client"
  description                       = "The distribution signs its reads of the web client's bucket."
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# Only this distribution reads the bucket, and only objects under play/.
resource "aws_s3_bucket_policy" "web_client" {
  bucket = aws_s3_bucket.web_client.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "RequireTls"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [aws_s3_bucket.web_client.arn, "${aws_s3_bucket.web_client.arn}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
      {
        Sid       = "ReadFromTheGameDistribution"
        Effect    = "Allow"
        Principal = { Service = "cloudfront.amazonaws.com" }
        Action    = "s3:GetObject"
        Resource  = "${aws_s3_bucket.web_client.arn}/play/*"
        Condition = { StringEquals = { "AWS:SourceArn" = aws_cloudfront_distribution.game.arn } }
      },
    ]
  })

  depends_on = [aws_s3_bucket_public_access_block.web_client]
}

data "aws_cloudfront_cache_policy" "caching_optimized" {
  name = "Managed-CachingOptimized"
}

# /play redirects to /play/, and a path that ends in a slash serves that
# folder's index.html. The query string (?join=CODE) isn't part of the URI, so
# it reaches the page untouched on /play/, and the redirect rebuilds it from
# request.querystring so /play?join=CODE keeps its code. Redirects come from
# the function and aren't cached. deploy/test_web_client_function.py runs this
# code in node.
resource "aws_cloudfront_function" "web_client" {
  name    = "${var.name}-web-client"
  runtime = "cloudfront-js-2.0"
  comment = "Serves /play/ as play/index.html and redirects /play to /play/."
  publish = true
  code    = <<-EOT
    function handler(event) {
      var request = event.request;
      if (request.uri === "/play") {
        var parts = [];
        var query = request.querystring || {};
        function decode(text) {
          try {
            return decodeURIComponent(text);
          } catch (e) {
            return text;
          }
        }
        function add(key, value) {
          var pair = encodeURIComponent(decode(key));
          if (value !== undefined && value !== "") {
            pair += "=" + encodeURIComponent(decode(value));
          }
          parts.push(pair);
        }
        Object.keys(query).forEach(function (key) {
          var entry = query[key];
          if (entry.multiValue) {
            entry.multiValue.forEach(function (item) {
              add(key, item.value);
            });
          } else {
            add(key, entry.value);
          }
        });
        return {
          statusCode: 301,
          statusDescription: "Moved Permanently",
          headers: {
            location: { value: "/play/" + (parts.length ? "?" + parts.join("&") : "") },
          },
        };
      }
      if (request.uri.endsWith("/")) {
        request.uri += "index.html";
      }
      return request;
    }
  EOT
}
