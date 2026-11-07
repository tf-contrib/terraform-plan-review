{
  description = "terraform-plan-review - Readable Terraform and OpenTofu plan reviews on pull requests";

  nixConfig = {
    extra-substituters = [ "https://ralch.cachix.org" ];
    extra-trusted-public-keys = [ "ralch.cachix.org-1:ti5RSqCJwd+VIv7dlxJEAVD98KIJDLxj1yJY+S80Q3A=" ];
  };

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = (pkgs.lib.importJSON ./.github/config/release-please-manifest.json).".";
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "terraform-plan-review";
          inherit version;
          src = pkgs.lib.cleanSource ./.;
          subPackages = [ "cmd/terraform-plan-review" ];
          vendorHash = "sha256-DkoCiB8Y4wZGhlN1R73Sqzk5wBxn9pXPendOzItFRgo=";
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          meta = with pkgs.lib; {
            description = "Readable Terraform and OpenTofu plan reviews on pull requests";
            license = licenses.mpl20;
            mainProgram = "terraform-plan-review";
          };
        };

        devShells.default = pkgs.mkShell {
          name = "terraform-plan-review";
          packages = [
            pkgs.go
            pkgs.opentofu
          ];
        };
      }
    );
}
