# scripts/aws.sh output <name> prints one of these. The VM deploy takes the
# static IP and the Function URL (deploy/README.md).

output "static_ip" {
  description = "The VM's static IP. The vm.<domain> A record points at it."
  value       = aws_lightsail_static_ip.vm.ip_address
}

output "function_url" {
  description = "The backend's public address, and lambda_function_url in deploy/local/vm.yml."
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
