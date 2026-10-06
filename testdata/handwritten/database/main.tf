# Configuration matching testdata/plans/database.json, used only for
# source-location tests. It is never planned.
resource "aws_db_instance" "main" {
  identifier = "orders-v2"
}

resource "aws_s3_bucket" "logs" {
  bucket = "example-logs"
  tags   = { team = "platform" }
}

resource "aws_instance" "web" {
  ami = "ami-00000000000000000"
}

import {
  to = aws_iam_role.ci
  id = "ci-role"
}

resource "aws_iam_role" "ci" {
  name = "ci-role"
}

data "aws_iam_policy_document" "assume" {}
