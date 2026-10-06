{
  description = "tofu-plan-review - Readable OpenTofu plan reviews on pull requests";

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
          pname = "tofu-plan-review";
          inherit version;
          src = pkgs.lib.cleanSource ./.;
          subPackages = [ "cmd/tofu-plan-review" ];
          vendorHash = "sha256-9GDJwS4gSmaIzJE6VUGnZjrbRDdFwj8r+6cHN3jcdVg=";
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          meta = with pkgs.lib; {
            description = "Readable OpenTofu plan reviews on pull requests";
            license = licenses.mpl20;
            mainProgram = "tofu-plan-review";
          };
        };

        devShells.default = pkgs.mkShell {
          name = "tofu-plan-review";
          packages = [
            pkgs.go
            pkgs.opentofu
          ];
        };
      }
    );
}
