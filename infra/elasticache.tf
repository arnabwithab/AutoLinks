# ElastiCache Serverless Redis: queue, jobs, graph, rate limits (§0 shared state).
resource "aws_security_group" "redis" {
  vpc_id      = aws_vpc.main.id
  description = "ElastiCache access from ECS tasks"
  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.tasks.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_elasticache_serverless_cache" "autolinks" {
  engine                   = "redis"
  name                     = "autolinks"
  major_engine_version     = "7"
  subnet_ids               = aws_subnet.private[*].id
  security_group_ids       = [aws_security_group.redis.id]
  daily_snapshot_time      = "06:00"
  snapshot_retention_limit = 1
}
