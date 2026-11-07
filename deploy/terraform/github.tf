# The role that the repository's Deploy workflow (.github/workflows/deploy.yml)
# assumes through GitHub's OIDC provider, so no AWS keys are stored in GitHub.
# Only jobs in var.github_environment of var.github_repository may assume it.
#
# It can read the account, as a plan must, and write only what a backend or VM
# deploy writes: the function's code and settings, the deploy bucket, and a
# temporary SSH key for the VM. Anything else, such as new alarms or a change
# to the VM, goes through `scripts/aws.sh apply`: on a person's machine, or in
# the Deploy workflow's apply target, which uses the apply role from
# deploy/github_apply_role.py after the owner's approval.

data "aws_caller_identity" "current" {}

locals {
  github_deploy = var.github_repository != null
}

resource "aws_iam_openid_connect_provider" "github" {
  count          = local.github_deploy ? 1 : 0
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

resource "aws_iam_role" "github_deploy" {
  count = local.github_deploy ? 1 : 0
  name  = "${var.name}-github-deploy"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github[0].arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          "token.actions.githubusercontent.com:sub" = "repo:${var.github_repository}:environment:${var.github_environment}"
        }
      }
    }]
  })
}

# Every plan refreshes every resource, across Lightsail, Lambda, IAM, logs,
# alarms, SNS, budgets, ACM, CloudFront and the backups bucket. The bucket's
# policy keeps the backups themselves from this role (backups.tf).
resource "aws_iam_role_policy_attachment" "github_deploy_read" {
  count      = local.github_deploy ? 1 : 0
  role       = aws_iam_role.github_deploy[0].name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

resource "aws_iam_role_policy" "github_deploy" {
  count = local.github_deploy ? 1 : 0
  name  = "deploy"
  role  = aws_iam_role.github_deploy[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "PublishTheBackend"
        Effect = "Allow"
        Action = [
          "lambda:UpdateFunctionCode",
          "lambda:UpdateFunctionConfiguration",
          "lambda:TagResource",
          "lambda:UntagResource",
        ]
        Resource = "arn:aws:lambda:${var.region}:${data.aws_caller_identity.current.account_id}:function:${var.name}"
      },
      {
        Sid      = "KeepTheFunctionsRole"
        Effect   = "Allow"
        Action   = "iam:PassRole"
        Resource = aws_iam_role.lambda.arn
        Condition = {
          StringEquals = { "iam:PassedToService" = "lambda.amazonaws.com" }
        }
      },
      {
        Sid      = "ListTheDeployBucket"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = "arn:aws:s3:::${var.deploy_bucket}"
      },
      {
        # The state, its lock file and deploy/local/'s files.
        Sid      = "SyncTheDeployBucket"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject"]
        Resource = "arn:aws:s3:::${var.deploy_bucket}/*"
      },
      {
        # Nothing else is deleted: Terraform only removes its lock.
        Sid      = "ReleaseTheStateLock"
        Effect   = "Allow"
        Action   = "s3:DeleteObject"
        Resource = "arn:aws:s3:::${var.deploy_bucket}/terraform/terraform.tfstate.tflock"
      },
      {
        Sid      = "LogInToTheVm"
        Effect   = "Allow"
        Action   = "lightsail:GetInstanceAccessDetails"
        Resource = "*"
      },
    ]
  })
}
