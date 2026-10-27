output "api_url" {
  description = "Base URL of the API; use as RIFTBOUND_API_URL."
  value       = aws_apigatewayv2_stage.default.invoke_url
}

output "cards_bucket" {
  value = aws_s3_bucket.cards.bucket
}

output "sync_function" {
  description = "Invoke once after the first deploy to populate the bucket."
  value       = aws_lambda_function.fn["sync"].function_name
}
