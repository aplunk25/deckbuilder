# Both functions are Go binaries named "bootstrap" on the provided.al2023
# runtime, built by `make lambda` into build/<name>/bootstrap.

locals {
  functions = {
    api = {
      timeout = 10
      actions = ["s3:GetObject"]
    }
    sync = {
      timeout = 120
      actions = ["s3:PutObject"]
    }
  }
}

data "archive_file" "fn" {
  for_each    = local.functions
  type        = "zip"
  source_file = "${local.build_dir}/${each.key}-lambda/bootstrap"
  output_path = "${local.build_dir}/${each.key}-lambda.zip"
}

data "aws_iam_policy_document" "lambda_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "fn" {
  for_each           = local.functions
  name               = "${var.project}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.lambda_trust.json
}

# Least privilege: each function gets only the S3 action it needs on the one
# object, plus permission to write its own logs.
data "aws_iam_policy_document" "fn" {
  for_each = local.functions
  statement {
    actions   = each.value.actions
    resources = ["${aws_s3_bucket.cards.arn}/cards.json"]
  }
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.fn[each.key].arn}:*"]
  }
}

resource "aws_iam_role_policy" "fn" {
  for_each = local.functions
  name     = "${var.project}-${each.key}"
  role     = aws_iam_role.fn[each.key].id
  policy   = data.aws_iam_policy_document.fn[each.key].json
}

resource "aws_cloudwatch_log_group" "fn" {
  for_each          = local.functions
  name              = "/aws/lambda/${var.project}-${each.key}"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "fn" {
  for_each         = local.functions
  function_name    = "${var.project}-${each.key}"
  role             = aws_iam_role.fn[each.key].arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  memory_size      = 256
  timeout          = each.value.timeout
  filename         = data.archive_file.fn[each.key].output_path
  source_code_hash = data.archive_file.fn[each.key].output_base64sha256

  environment {
    variables = {
      CARDS_BUCKET = aws_s3_bucket.cards.bucket
      CARDS_KEY    = "cards.json"
    }
  }

  depends_on = [aws_cloudwatch_log_group.fn, aws_iam_role_policy.fn]
}

# ---- daily card sync ----

resource "aws_cloudwatch_event_rule" "sync" {
  name                = "${var.project}-sync"
  description         = "Refresh card data from the Riftcodex API"
  schedule_expression = var.sync_schedule
}

resource "aws_cloudwatch_event_target" "sync" {
  rule = aws_cloudwatch_event_rule.sync.name
  arn  = aws_lambda_function.fn["sync"].arn
}

resource "aws_lambda_permission" "sync_schedule" {
  statement_id  = "AllowEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.fn["sync"].function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.sync.arn
}
