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

# Caches an answer for as long as the origin's Cache-Control says, and not at
# all without one or with no-store. The managed UseOriginCacheControlHeaders
# policy does the same but puts the viewer's Host header and every cookie in
# the cache key, and CloudFront sends cache-key headers to the origin: a
# Function URL refuses a request whose Host isn't its own. So this policy
# keys on the path alone (and the encoding CloudFront compresses to).
resource "aws_cloudfront_cache_policy" "origin_cache_control" {
  name        = "${var.name}-origin-cache-control"
  comment     = "Follows the origin's Cache-Control; the cache key is the path."
  min_ttl     = 0
  default_ttl = 0
  max_ttl     = 31536000

  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true

    cookies_config {
      cookie_behavior = "none"
    }
    headers_config {
      header_behavior = "none"
    }
    query_strings_config {
      query_string_behavior = "none"
    }
  }
}

# The public leaderboard (#367): its answers say Cache-Control: public,
# max-age=60, which this policy follows, so a visitor costs the database at
# most one read a minute per page. Unlike origin_cache_control, the key holds
# the query string's parameters, which choose the page, and the Origin header,
# since the backend's CORS answer varies by it (Vary: Origin). Neither is Host,
# so the Function URL still gets its own.
resource "aws_cloudfront_cache_policy" "leaderboard" {
  name        = "${var.name}-leaderboard"
  comment     = "Follows the origin's Cache-Control; the cache key is the path, kind, limit, offset and Origin."
  min_ttl     = 0
  default_ttl = 0
  max_ttl     = 3600

  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true

    cookies_config {
      cookie_behavior = "none"
    }
    headers_config {
      header_behavior = "whitelist"
      headers {
        items = ["Origin"]
      }
    }
    query_strings_config {
      query_string_behavior = "whitelist"
      query_strings {
        items = ["kind", "limit", "offset"]
      }
    }
  }
}

# Every header, cookie and query string except Host, which the Function URL
# needs to be its own. CloudFront strips Authorization from GET and HEAD
# requests unless the policy forwards all viewer headers, so the smoke test
# checks an authenticated GET.
data "aws_cloudfront_origin_request_policy" "all_viewer_except_host_header" {
  name = "Managed-AllViewerExceptHostHeader"
}

# Except for /play/ (the web client, from S3), the static files of the
# Lambda's pages and the public leaderboard (below), nothing is cached, and /lobby/ui/ redirects to the
# lobby: every request reaches the Lambda
# as if it came straight to the Function URL. There's no origin access
# control. For a Function URL it
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

  # The lobby's page moved to lobby_hostname (#232). Old links to it, such as
  # invitations, reach the lobby_redirect function below and never the
  # backend, which answers 410 for the rest of /lobby/*.
  dynamic "ordered_cache_behavior" {
    for_each = ["/lobby/ui/*", "/lobby/ui"]

    content {
      path_pattern           = ordered_cache_behavior.value
      target_origin_id       = "backend"
      viewer_protocol_policy = "redirect-to-https"
      allowed_methods        = ["GET", "HEAD", "OPTIONS"]
      cached_methods         = ["GET", "HEAD"]
      cache_policy_id        = data.aws_cloudfront_cache_policy.caching_disabled.id

      function_association {
        event_type   = "viewer-request"
        function_arn = aws_cloudfront_function.lobby_redirect.arn
      }
    }
  }

  # The pages the Lambda serves besides the API (#162): the game's modules,
  # the demo viewer and the map builder. Their handlers say in Cache-Control
  # what may be kept: scripts and styles for five minutes, the HTML not at
  # all, so this policy obeys the origin. No API route lies under these
  # prefixes, so API answers stay uncached.
  dynamic "ordered_cache_behavior" {
    for_each = ["/game/demo/*", "/game/demo-viewer/*", "/map/builder/*"]

    content {
      path_pattern             = ordered_cache_behavior.value
      target_origin_id         = "backend"
      viewer_protocol_policy   = "redirect-to-https"
      allowed_methods          = ["GET", "HEAD", "OPTIONS"]
      cached_methods           = ["GET", "HEAD"]
      cache_policy_id          = aws_cloudfront_cache_policy.origin_cache_control.id
      origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
      compress                 = true
    }
  }

  # The public leaderboard (#367), GET /leaderboard and a competitor's page,
  # GET /leaderboard/{competitor_id}: the backend says Cache-Control: public,
  # max-age=60, so each page reaches the Lambda at most once a minute per
  # query and Origin. The admin's routes are under /admin/, uncached.
  ordered_cache_behavior {
    path_pattern             = "/leaderboard*"
    target_origin_id         = "backend"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["GET", "HEAD", "OPTIONS"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = aws_cloudfront_cache_policy.leaderboard.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
    compress                 = true
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

# The lobby's page moved to its own name (#232). A request for /lobby/ui or
# under it, such as an old invitation link, gets a permanent redirect to the
# same path and query string on lobby_hostname; an invitation's code is in the
# URL's fragment, which the browser keeps across the redirect. Redirects come
# from the function, so neither CloudFront nor the backend sees them.
# deploy/test_lobby_redirect_function.py runs this code in node.
resource "aws_cloudfront_function" "lobby_redirect" {
  name    = "${var.name}-lobby-redirect"
  runtime = "cloudfront-js-2.0"
  comment = "Redirects the lobby's old page to the lobby's own name."
  publish = true
  code    = <<-EOT
    var lobby = "https://${var.lobby_hostname}";

    function handler(event) {
      var request = event.request;
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
      var path = request.uri === "/lobby/ui" ? "/lobby/ui/" : request.uri;
      return {
        statusCode: 301,
        statusDescription: "Moved Permanently",
        headers: {
          location: { value: lobby + path + (parts.length ? "?" + parts.join("&") : "") },
          "cache-control": { value: "max-age=86400" },
        },
      };
    }
  EOT
}
