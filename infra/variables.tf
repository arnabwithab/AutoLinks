# Shared inputs for the distributed stack.
variable "region" {
  description = "AWS region (Qdrant Cloud + Vercel stay outside Terraform)."
  type        = string
  default     = "us-west-1"
}

variable "image_tag" {
  description = "ECR image tag for api/worker (one image, command selects role)."
  type        = string
  default     = "latest"
}

variable "api_desired" {
  type    = number
  default = 2
}

variable "worker_desired" {
  type    = number
  default = 2
}

variable "task_cpu" {
  description = "Fargate CPU units (256 = 0.25 vCPU)."
  type        = string
  default     = "512"
}

variable "task_memory" {
  type    = string
  default = "1024"
}

variable "qdrant_url" {
  description = "Qdrant Cloud gRPC endpoint (stays managed outside Terraform)."
  type        = string
}

variable "models_backend" {
  description = "hf until the SageMaker endpoint is warm, then sagemaker."
  type        = string
  default     = "hf"
}

variable "enable_sagemaker" {
  description = "Provision the SageMaker inference endpoint (needs model_data_url)."
  type        = bool
  default     = false
}

variable "sagemaker_model_data_url" {
  description = "S3 URL of the GLiNER+MiniLM model tarball."
  type        = string
  default     = ""
}

variable "clerk_secret_key" {
  type      = string
  sensitive = true
}

variable "qdrant_api_key" {
  type      = string
  sensitive = true
}

variable "hf_token" {
  type      = string
  sensitive = true
}

variable "groq_api_key" {
  type      = string
  sensitive = true
  default     = ""
}
