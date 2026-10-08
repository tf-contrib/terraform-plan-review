{
  description = "terraform-plan-review - Readable Terraform and OpenTofu plan reviews on pull requests";

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
          vendorHash = "sha256-d0/mmonSbZNTr/RMVlGQ/MzQcpQiLcIie98Bc9aGpsU=";
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
