# The game's name (#75): players open https://<game_hostname>, which a
# CloudFront distribution serves from the backend's Function URL. A DNS record
# alone can't do it: a Function URL's certificate only covers
# *.lambda-url.<region>.on.aws, and it expects its own Host. Squarespace has
# no DNS API, so the certificate's validation record and the name's CNAME are
# added by hand (deploy/README.md).

locals {
  # https://<id>.lambda-url.<region>.on.aws/ without the scheme and the slash.
  function_url_host = split("/", aws_lambda_function_url.backend.function_url)[2]
}

# CloudFront only takes certificates from us-east-1. The first apply requests
# it on its own, with -target, so its validation record can be added before
# anything waits for it (deploy/README.md). ACM renews the certificate only
# while that record stays in place.
resource "aws_acm_certificate" "game" {
  region            = "us-east-1"
  domain_name       = var.game_hostname
  validation_method = "DNS"

  lifecycle {
    # A new name gets its certificate before the distribution lets go of the
    # old one.
    create_before_destroy = true
  }
}

# Waits until ACM has seen the validation record and issued the certificate.
resource "aws_acm_certificate_validation" "game" {
  region          = "us-east-1"
  certificate_arn = aws_acm_certificate.game.arn
}

data "aws_cloudfront_cache_policy" "caching_disabled" {
  name = "Managed-CachingDisabled"
}

# Every header, cookie and query string except Host, which the Function URL
# needs to be its own. CloudFront strips Authorization from GET and HEAD
# requests unless the policy forwards all viewer headers, so the smoke test
# checks an authenticated GET.
data "aws_cloudfront_origin_request_policy" "all_viewer_except_host_header" {
  name = "Managed-AllViewerExceptHostHeader"
}

# Except for /play/ (the web client, from S3), nothing is cached: every request reaches the Lambda as if it came straight
# to the Function URL. There's no origin access control. For a Function URL it
# would need clients to send a SHA-256 of every POST body, and the Function
# URL is public anyway. Standard and real-time logs stay off.
resource "aws_cloudfront_distribution" "game" {
  enabled         = true
  comment         = "${var.name}: the backend Lambda at ${var.game_hostname}"
  aliases         = [var.game_hostname]
  is_ipv6_enabled = true
  # North America, Europe, Israel and Türkiye: the cheapest edges, should the
  # free tier ever run out. The origin is in one region, and nothing is
  # cached, so further edges would gain little.
  price_class = "PriceClass_100"

  origin {
    origin_id   = "backend"
    domain_name = local.function_url_host

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
      # How long CloudFront waits for the first byte, and between the parts of
      # a streamed answer. The most it allows without a quota increase. A GET
      # that runs out of it would be retried, so lambda_timeout_seconds stays
      # under it with room for a cold start.
      origin_read_timeout = 60
    }
  }

  # The web client (web_client.tf): a private bucket through an origin access
  # control, placed before the default behaviour. "/play/*" doesn't match
  # "/play" itself, so that has a behaviour too, for the function's redirect.
  origin {
    origin_id                = "web-client"
    domain_name              = aws_s3_bucket.web_client.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.web_client.id
  }

  dynamic "ordered_cache_behavior" {
    for_each = ["/play/*", "/play"]

    content {
      path_pattern           = ordered_cache_behavior.value
      target_origin_id       = "web-client"
      viewer_protocol_policy = "redirect-to-https"
      allowed_methods        = ["GET", "HEAD", "OPTIONS"]
      cached_methods         = ["GET", "HEAD"]
      cache_policy_id        = data.aws_cloudfront_cache_policy.caching_optimized.id
      compress               = true

      function_association {
        event_type   = "viewer-request"
        function_arn = aws_cloudfront_function.web_client.arn
      }
    }
  }

  default_cache_behavior {
    target_origin_id         = "backend"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.game.certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }
}
