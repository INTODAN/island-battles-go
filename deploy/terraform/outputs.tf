# scripts/aws.sh output <name> prints one of these. The DNS records take the
# certificate's validation record and the distribution's domain
# (deploy/README.md). The VM's static IP is INTODAN/mpl's output since #232.

output "function_url" {
  description = "The backend's own address, which CloudFront calls."
  value       = aws_lambda_function_url.backend.function_url
}

output "function_name" {
  description = "The backend Lambda's name."
  value       = aws_lambda_function.backend.function_name
}

output "game_url" {
  description = "Where players open the game."
  value       = local.game_origin
}

output "lobby_url" {
  description = "The lobby's URL, which the 3D client's config.json names (scripts/deploy-web-client.sh)."
  value       = local.lobby_origin
}

output "certificate_validation_record" {
  description = "The CNAME that lets ACM issue the game's certificate and renew it. Keep it in the domain's DNS."
  value       = join("\n", [for option in aws_acm_certificate.game.domain_validation_options : "${option.resource_record_name} CNAME ${option.resource_record_value}"])
}

output "distribution_domain" {
  description = "The CloudFront distribution's own name, which the game_hostname CNAME points at."
  value       = aws_cloudfront_distribution.game.domain_name
}

output "web_client_bucket" {
  description = "The S3 bucket that holds the web client, under play/. scripts/deploy-web-client.sh uploads to it."
  value       = aws_s3_bucket.web_client.bucket
}

output "distribution_id" {
  description = "The CloudFront distribution's ID, which scripts/deploy-web-client.sh invalidates."
  value       = aws_cloudfront_distribution.game.id
}

output "database_backup_bucket" {
  description = "The S3 bucket that holds the old daily database backups, under postgres/, until they expire."
  value       = aws_s3_bucket.backups.bucket
}

output "github_deploy_role_arn" {
  description = "The role the Deploy workflow assumes: the repository's AWS_DEPLOY_ROLE_ARN variable. Null without github_repository."
  value       = try(aws_iam_role.github_deploy[0].arn, null)
}
