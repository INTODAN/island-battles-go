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

  # The state lives in the deploy bucket, so any machine with the AWS
  # credentials can apply. It holds the Lambda's environment, secrets included.
  # scripts/aws.sh passes the bucket and its region, which come from the
  # account (deploy/deploy_files.py). The lock file stops two applies at once.
  backend "s3" {
    key          = "terraform/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
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
