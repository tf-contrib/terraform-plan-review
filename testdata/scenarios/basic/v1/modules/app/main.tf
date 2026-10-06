variable "replicas" { type = number }
resource "terraform_data" "deployment" {
  input = { replicas = var.replicas, image = "registry.example.com/app:1.0.0" }
}
