resource "terraform_data" "policy" {
  input = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Sid = "Read", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::example-bucket/*" }]
  })
}
resource "terraform_data" "server" {
  input            = { size = "large", tags = { env = "dev", team = "platform" } }
  triggers_replace = ["v2"]
}
resource "terraform_data" "renamed_to" { input = "x" }
moved {
  from = terraform_data.renamed_from
  to   = terraform_data.renamed_to
}
resource "terraform_data" "secret" { input = sensitive("not-a-real-secret-2") }
resource "terraform_data" "kept" { input = "same" }
removed {
  from = terraform_data.forgotten
  lifecycle { destroy = false }
}
resource "terraform_data" "list" { input = ["10.0.0.0/24", "10.0.2.0/24", "10.0.3.0/24"] }
resource "terraform_data" "new" { input = terraform_data.server.id }
module "app" {
  source   = "./modules/app"
  replicas = 3
}
output "server_size" { value = terraform_data.server.output.size }
output "new_id" { value = terraform_data.new.id }
