# Secrets Manager: keys injected as env vars; never baked into the image.
resource "aws_secretsmanager_secret" "app" {
  name = "autolinks/app"
}

resource "aws_secretsmanager_secret_version" "app" {
  secret_id = aws_secretsmanager_secret.app.id
  secret_string = jsonencode({
    CLERK_SECRET_KEY = var.clerk_secret_key
    QDRANT_API_KEY   = var.qdrant_api_key
    HF_TOKEN         = var.hf_token
    GROQ_API_KEY     = var.groq_api_key
  })
}

resource "aws_iam_role" "execution" {
  name = "autolinks-execution"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "secrets" {
  role = aws_iam_role.execution.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action   = ["secretsmanager:GetSecretValue"]
      Effect   = "Allow"
      Resource = aws_secretsmanager_secret.app.arn
    }]
  })
}
