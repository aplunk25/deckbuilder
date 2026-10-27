terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }

  # Partial config: the bucket is passed at init time, e.g.
  #   terraform init -backend-config="bucket=deckbuilder-tfstate-<account id>"
  # use_lockfile gives state locking with S3 alone (no DynamoDB table).
  backend "s3" {
    key          = "deckbuilder/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = var.project, ManagedBy = "terraform" }
  }
}

data "aws_caller_identity" "current" {}

locals {
  build_dir = "${path.module}/../build"
}
