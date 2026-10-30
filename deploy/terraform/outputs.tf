# scripts/aws.sh output <name> prints one of these. The VM deploy takes the
# static IP, the Function URL and the game's URL. The DNS records take the
# static IP, the certificate's validation record and the distribution's
# domain (deploy/README.md).

output "static_ip" {
  description = "The VM's static IP. The vm.<domain> A record points at it."
  value       = aws_lightsail_static_ip.vm.ip_address
}

output "function_url" {
  description = "The backend's own address, which CloudFront and Centrifugo's proxy calls use, and lambda_function_url in deploy/local/vm.yml."
  value       = aws_lambda_function_url.backend.function_url
}

output "function_name" {
  description = "The backend Lambda's name."
  value       = aws_lambda_function.backend.function_name
}

output "instance_ips" {
  description = "Each instance's public IP. The instance the static IP is attached to shows the static IP."
  value       = { for name, instance in aws_lightsail_instance.vm : name => instance.public_ip_address }
}

output "game_url" {
  description = "Where players open the game, and game_url in deploy/local/vm.yml."
  value       = "https://${var.game_hostname}"
}

output "certificate_validation_record" {
  description = "The CNAME that lets ACM issue the game's certificate and renew it. Keep it in the domain's DNS."
  value       = join("\n", [for option in aws_acm_certificate.game.domain_validation_options : "${option.resource_record_name} CNAME ${option.resource_record_value}"])
}

output "distribution_domain" {
  description = "The CloudFront distribution's own name, which the game_hostname CNAME points at."
  value       = aws_cloudfront_distribution.game.domain_name
}
