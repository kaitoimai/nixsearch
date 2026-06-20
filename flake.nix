{
  description = "Search nixpkgs packages and show Darwin availability";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = {
    self,
    nixpkgs,
  }: let
    systems = [
      "aarch64-darwin"
      "x86_64-darwin"
      "x86_64-linux"
      "aarch64-linux"
    ];

    forAllSystems = nixpkgs.lib.genAttrs systems;
  in {
    packages = forAllSystems (system: let
      pkgs = import nixpkgs {
        inherit system;
      };
      buildGo126Module =
        if pkgs ? buildGo126Module
        then pkgs.buildGo126Module
        else pkgs.buildGoModule.override {go = pkgs.go_1_26;};
    in {
      default = buildGo126Module {
        pname = "nixsearch";
        version = "0.1.0";
        src = ./.;
        vendorHash = null;

        subPackages = [
          "cmd/nixsearch"
        ];
      };
    });

    apps = forAllSystems (system: {
      default = {
        type = "app";
        program = "${self.packages.${system}.default}/bin/nixsearch";
        meta.description = "Search nixpkgs packages and show Darwin availability";
      };
    });

    checks = forAllSystems (system: let
      pkgs = import nixpkgs {
        inherit system;
      };
    in {
      smoke =
        pkgs.runCommand "nixsearch-smoke" {
          nativeBuildInputs = [
            self.packages.${system}.default
          ];
        } ''
          nixsearch --help >/dev/null
          touch "$out"
        '';
    });

    devShells = forAllSystems (system: let
      pkgs = import nixpkgs {
        inherit system;
      };
    in {
      default = pkgs.mkShell {
        packages = [
          pkgs.alejandra
          pkgs.go_1_26
        ];
      };
    });

    formatter = forAllSystems (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in
      pkgs.writeShellApplication {
        name = "format-nixsearch";
        runtimeInputs = [
          pkgs.alejandra
        ];
        text = ''
          if [ "$#" -eq 0 ]; then
            exec alejandra flake.nix
          fi

          exec alejandra "$@"
        '';
      });
  };
}
