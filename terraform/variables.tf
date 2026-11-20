variable "aws_region" {
  description = "Região da AWS onde toda a infraestrutura será provisionada"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Prefixo padrão dos recursos do projeto"
  type        = string
  default     = "togglemaster"
}

variable "cluster_name" {
  description = "Nome do cluster Amazon EKS"
  type        = string
  default     = "toggle-master-cluster"
}

variable "db_username" {
  description = "Usuário administrador das instâncias RDS PostgreSQL"
  type        = string
  default     = "postgres"
}

variable "db_password" {
  description = "Senha das instâncias RDS PostgreSQL (armazenada no AWS Secrets Manager e K8s Secrets)"
  type        = string
  default     = "ToggleMaster2026SecureDB!"
  sensitive   = true
}

variable "master_key" {
  description = "Chave mestra administrativa do auth-service"
  type        = string
  default     = "admin-secreto-123"
  sensitive   = true
}

variable "service_api_key" {
  description = "Chave de API interna entre microsserviços"
  type        = string
  default     = "tm_key_evaluation_service_secret"
  sensitive   = true
}

variable "datadog_api_key" {
  description = "API Key do Datadog para exportação de traces e métricas APM via OTel Collector"
  type        = string
  default     = ""
  sensitive   = true
}

variable "datadog_site" {
  description = "Site regional do Datadog (ex.: datadoghq.com)"
  type        = string
  default     = "datadoghq.com"
}

variable "pagerduty_routing_key" {
  description = "Integration Key (Events API v2) do serviço ToggleMaster no PagerDuty"
  type        = string
  default     = ""
  sensitive   = true
}

variable "grafana_admin_password" {
  description = "Senha do administrador do Grafana"
  type        = string
  default     = "ToggleMasterGrafana2026!"
  sensitive   = true
}
