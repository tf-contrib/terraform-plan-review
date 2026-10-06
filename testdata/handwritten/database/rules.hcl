override_label = "destroy-approved"

rule "protect-databases" {
  severity       = "block"
  message        = "Databases must not be replaced without approval."
  resource_types = ["aws_db_*"]
  actions        = ["delete", "replace"]
}

rule "watch-iam" {
  severity       = "warn"
  message        = "IAM change: request a security review."
  resource_types = ["aws_iam_*"]
}

rule "hide-tags-all" {
  severity   = "ignore"
  attributes = ["tags_all"]
}
