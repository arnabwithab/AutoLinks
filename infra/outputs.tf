output "alb_dns" {
  description = "Point frontend / DNS at this (or an ACM alias)."
  value       = aws_lb.api.dns_name
}

output "ecr_repo" {
  value = aws_ecr_repository.app.repository_url
}

output "redis_endpoint" {
  value     = aws_elasticache_serverless_cache.autolinks.endpoint
  sensitive = true
}

output "sagemaker_endpoint_name" {
  value = var.enable_sagemaker ? aws_sagemaker_endpoint.infer[0].name : ""
}
