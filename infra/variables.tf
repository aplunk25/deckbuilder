variable "region" {
  type    = string
  default = "us-east-1"
}

variable "project" {
  description = "Prefix for resource names."
  type        = string
  default     = "deckbuilder"
}

variable "sync_schedule" {
  description = "EventBridge schedule for refreshing card data."
  type        = string
  default     = "cron(0 6 * * ? *)" # daily, 06:00 UTC
}

variable "log_retention_days" {
  type    = number
  default = 14
}

variable "api_throttle_rate" {
  description = "Steady-state requests per second allowed by API Gateway."
  type        = number
  default     = 10
}

variable "api_throttle_burst" {
  type    = number
  default = 20
}
