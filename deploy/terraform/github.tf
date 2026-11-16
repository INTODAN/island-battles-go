# The role that the repository's Deploy workflow (.github/workflows/deploy.yml)
# assumes through GitHub's OIDC provider, so no AWS keys are stored in GitHub.
# Only jobs in var.github_environment of var.github_repository may assume it.
#
# It can read the account, as a plan must, and write only what a backend
# deploy writes: the function's code and settings, the bot runners' code
# (bot_runners.tf), its own part of the deploy bucket (the state and its lock,
# files/ and plans/), and the web client's files under play/ and their cache
# invalidation. It writes nothing under mpl/, INTODAN/mpl's part of the deploy
# bucket, and reads only mpl/tenants/island-battles/tenant.env there, for the
# lambda-env target: a Deny keeps it out of mpl's files, state and plans
# (#264). The VM is INTODAN/mpl's (#232). Anything else, such as new alarms,
# goes through `scripts/aws.sh apply`: on a person's machine, or in the Deploy
# workflow's apply target, which applies a saved plan from main with the apply
# role from deploy/github_apply_role.py.

data "aws_caller_identity" "current" {}

locals {
  github_deploy = var.github_repository != null
  # INTODAN/mpl's part of the deploy bucket that neither GitHub role may read
  # (#264): its files with its secrets, its Terraform state and lock, its
  # saved plans, and the VM owner marker. They are the keys of mpl's
  # deploy/deploy_files.py (FILES_PREFIX, STATE_KEY, PLANS_PREFIX and
  # VM_OWNER_KEY), so a new key there needs adding here.
  #
  # mpl/tenants/ isn't listed, because the lambda-env target reads
  # mpl/tenants/island-battles/tenant.env. A Deny on all of mpl/* would deny
  # that file too, and no Allow overrides a Deny. IAM can't name "mpl/*
  # except mpl/tenants/island-battles/*" either: Resource has only the * and
  # ? wildcards, no S3 condition key holds an object's key, and a Deny with
  # NotResource would deny reading every other bucket's objects too.
  #
  # deploy/github_apply_role.py denies the apply role the same keys
  # (MPL_UNREADABLE_KEYS).
  mpl_unreadable_keys = ["mpl/files/*", "mpl/terraform/*", "mpl/plans/*", "mpl/vm-owner"]
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

# Every plan refreshes every resource, across Lambda, IAM, logs,
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
        # The bot runners' code (bot_runners.tf), which a backend deploy
        # publishes with the backend. Their settings change only on an apply.
        Sid      = "PublishTheBotRunners"
        Effect   = "Allow"
        Action   = "lambda:UpdateFunctionCode"
        Resource = [for name in values(local.bot_runner_function_names) : "arn:aws:lambda:${var.region}:${data.aws_caller_identity.current.account_id}:function:${name}"]
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
      # The deploy bucket is shared: INTODAN/mpl keeps its state, files,
      # plans and tenants' settings under mpl/. So the grants below name this
      # repository's keys, never the whole bucket. ReadOnlyAccess already
      # allows s3:GetObject everywhere, so what they limit is the writes, and
      # KeepOutOfMplsPart the reads.
      # Each key comes from elsewhere, and a change there must change this
      # policy too: the state's from the S3 backend's key in versions.tf
      # (STATE_KEY in deploy/deploy_files.py), the others from FILES_PREFIX,
      # PLANS_PREFIX and TENANT_KEY in deploy/deploy_files.py.
      {
        # Terraform's state and its lock file (use_lockfile), and
        # deploy/local/'s files, which the scripts pull and push.
        Sid    = "SyncItsPartOfTheDeployBucket"
        Effect = "Allow"
        Action = ["s3:GetObject", "s3:PutObject"]
        Resource = [
          "arn:aws:s3:::${var.deploy_bucket}/terraform/terraform.tfstate",
          "arn:aws:s3:::${var.deploy_bucket}/terraform/terraform.tfstate.tflock",
          "arn:aws:s3:::${var.deploy_bucket}/files/*",
        ]
      },
      {
        # The plan target's "Save the plan" (.github/workflows/deploy.yml).
        # The apply target reads and deletes it with the apply role.
        Sid      = "SaveThePlans"
        Effect   = "Allow"
        Action   = "s3:PutObject"
        Resource = "arn:aws:s3:::${var.deploy_bucket}/plans/*"
      },
      {
        # The settings that INTODAN/mpl publishes for Island Battles, its
        # tenant, which the lambda-env target copies (deploy_files.py tenant).
        Sid      = "ReadTheTenantSettings"
        Effect   = "Allow"
        Action   = "s3:GetObject"
        Resource = "arn:aws:s3:::${var.deploy_bucket}/mpl/tenants/island-battles/tenant.env"
      },
      {
        # ReadOnlyAccess reads every bucket, so an explicit Deny keeps
        # INTODAN/mpl's secrets, state and plans from this role.
        # s3:GetObject* is every read of an object: its content, the earlier
        # versions that the bucket keeps (GetObjectVersion), tags and ACLs.
        # Listing the keys stays allowed, and shows each object's ETag, its
        # MD5 for these uploads.
        Sid      = "KeepOutOfMplsPart"
        Effect   = "Deny"
        Action   = "s3:GetObject*"
        Resource = [for key in local.mpl_unreadable_keys : "arn:aws:s3:::${var.deploy_bucket}/${key}"]
      },
      {
        # Nothing else is deleted: Terraform only removes its lock.
        Sid      = "ReleaseTheStateLock"
        Effect   = "Allow"
        Action   = "s3:DeleteObject"
        Resource = "arn:aws:s3:::${var.deploy_bucket}/terraform/terraform.tfstate.tflock"
      },
      {
        # scripts/deploy-web-client.sh syncs the build into the web client
        # bucket (web_client.tf). The bucket policy there lets the
        # distribution read play/* and nothing else.
        Sid      = "ListTheWebClientBucket"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.web_client.arn
      },
      {
        Sid      = "UploadTheWebClient"
        Effect   = "Allow"
        Action   = ["s3:PutObject"]
        Resource = "${aws_s3_bucket.web_client.arn}/play/*"
      },
      {
        Sid      = "InvalidateTheWebClient"
        Effect   = "Allow"
        Action   = "cloudfront:CreateInvalidation"
        Resource = aws_cloudfront_distribution.game.arn
      },
    ]
  })
}
