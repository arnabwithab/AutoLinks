# ECS cluster with two services from one image; command selects the role.
resource "aws_ecs_cluster" "main" {
  name = "autolinks"
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/autolinks"
  retention_in_days = 14
}

locals {
  base_env = [
    { name = "PORT", value = "8000" },
    { name = "QDRANT_URL", value = var.qdrant_url },
    { name = "QDRANT_COLLECTION", value = "articles" },
    { name = "MODELS_BACKEND", value = var.models_backend },
    { name = "SAGEMAKER_ENDPOINT", value = var.enable_sagemaker ? aws_sagemaker_endpoint.infer[0].name : "" },
    { name = "REDIS_URL", value = "rediss://${aws_elasticache_serverless_cache.autolinks.endpoint[0].address}:${tostring(aws_elasticache_serverless_cache.autolinks.endpoint[0].port)}" },
    { name = "FRONTEND_URL", value = "https://autolinks.vercel.app" },
  ]
  secret_env = [
    { name = "CLERK_SECRET_KEY", valueFrom = "${aws_secretsmanager_secret.app.arn}:CLERK_SECRET_KEY::" },
    { name = "QDRANT_API_KEY", valueFrom = "${aws_secretsmanager_secret.app.arn}:QDRANT_API_KEY::" },
    { name = "HF_TOKEN", valueFrom = "${aws_secretsmanager_secret.app.arn}:HF_TOKEN::" },
    { name = "GROQ_API_KEY", valueFrom = "${aws_secretsmanager_secret.app.arn}:GROQ_API_KEY::" },
  ]
}

resource "aws_ecs_task_definition" "api" {
  family                   = "autolinks-api"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.execution.arn
  container_definitions = jsonencode([{
    name         = "api"
    image        = "${aws_ecr_repository.app.repository_url}:${var.image_tag}"
    command      = ["/api"]
    portMappings = [{ containerPort = 8000 }]
    environment  = local.base_env
    secrets      = local.secret_env
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group  = aws_cloudwatch_log_group.app.name
        awslogs-region = var.region
        awslogs-stream-prefix = "api"
      }
    }
  }])
}

resource "aws_ecs_task_definition" "worker" {
  family                   = "autolinks-worker"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.execution.arn
  container_definitions = jsonencode([{
    name         = "worker"
    image        = "${aws_ecr_repository.app.repository_url}:${var.image_tag}"
    command      = ["/worker"]
    environment  = local.base_env
    secrets      = local.secret_env
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group  = aws_cloudwatch_log_group.app.name
        awslogs-region = var.region
        awslogs-stream-prefix = "worker"
      }
    }
  }])
}

resource "aws_ecs_service" "api" {
  name            = "autolinks-api"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = var.api_desired
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = false
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = 8000
  }
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
}

resource "aws_ecs_service" "worker" {
  name            = "autolinks-worker"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.worker.arn
  desired_count   = var.worker_desired
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = false
  }
  # Scale-in protection lives here once AWS supports it per-service;
  # until then SIGTERM drain + lease reclaim (§7) covers replacement.
}
