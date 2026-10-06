resource "terraform_data" "policy" {
  input = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Sid = "Read", Effect = "Allow", Action = "s3:GetObject", Resource = "arn:aws:s3:::example-bucket/*" }]
  })
}
resource "terraform_data" "server" {
  input            = { size = "small", tags = { env = "dev", owner = "team-a" } }
  triggers_replace = ["v1"]
}
resource "terraform_data" "old" { input = "to be deleted" }
resource "terraform_data" "renamed_from" { input = "x" }
resource "terraform_data" "secret" { input = sensitive("not-a-real-secret-1") }
resource "terraform_data" "kept" { input = "same" }
resource "terraform_data" "forgotten" { input = "f" }
resource "terraform_data" "list" { input = ["10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"] }
module "app" {
  source   = "./modules/app"
  replicas = 2
}
output "server_size" { value = terraform_data.server.output.size }
