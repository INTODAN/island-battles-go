# Observability for the backend Lambda (#46), within AWS's free monthly
# allowances. #46 adds its resources to this file:
#
# - the log group /aws/lambda/<function name> with a retention period. Without
#   it, the function creates its own log group on first use and keeps logs
#   forever.
# - a metric filter on the backend's JSON log lines
# - error alarms
# - a budget
#
# The function is aws_lambda_function.backend, named var.name. Add the log
# group before the function's first invocation, or import the one Lambda
# created.
