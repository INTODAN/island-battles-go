terraform {
  # 1.9 for variable validations that refer to other variables, and 1.11 for
  # override_during in the tests.
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.7"
    }
  }

  # The state lives with the other deploy-local files, which are gitignored and
  # backed up together. It holds the Lambda's environment, secrets included.
  # The path is relative to this directory, which scripts/aws.sh runs
  # Terraform in.
  backend "local" {
    path = "../local/terraform/terraform.tfstate"
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project   = "island-battles"
      ManagedBy = "terraform"
    }
  }
}
