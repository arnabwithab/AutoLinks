# SageMaker shared inference endpoint (§5). Off by default: MODELS_BACKEND=hf
# until the endpoint is warm with autoscaling replicas, then flip the flag.
# ponytail: image + model tarball are placeholders until the inference
# container is built; count=0 keeps apply green meanwhile.
variable "sagemaker_image" {
  description = "Inference container (GLiNER + MiniLM, micro-batching)."
  type        = string
  default     = ""
}

resource "aws_sagemaker_model" "infer" {
  count              = var.enable_sagemaker ? 1 : 0
  name               = "autolinks-infer"
  execution_role_arn = aws_iam_role.execution.arn
  primary_container {
    image          = var.sagemaker_image
    model_data_url = var.sagemaker_model_data_url
  }
}

resource "aws_sagemaker_endpoint_configuration" "infer" {
  count = var.enable_sagemaker ? 1 : 0
  name  = "autolinks-infer"
  production_variants {
    variant_name           = "primary"
    model_name             = aws_sagemaker_model.infer[0].name
    initial_instance_count = 1
    instance_type          = "ml.g5.xlarge"
  }
}

resource "aws_sagemaker_endpoint" "infer" {
  count                = var.enable_sagemaker ? 1 : 0
  name                 = "autolinks-infer"
  endpoint_config_name = aws_sagemaker_endpoint_configuration.infer[0].name
}
