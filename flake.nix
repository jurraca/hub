{
  description = "Alby Hub";

  inputs.nixpkgs.url = "nixpkgs/nixos-24.05";

  outputs = {
    self,
    nixpkgs,
  }: let
    # to work with older version of flakes
    lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";

    # Generate a user-friendly version number.
    version = builtins.substring 0 8 lastModifiedDate;

    # System types to support.
    supportedSystems = ["x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin"];

    # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
    forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

    # Nixpkgs instantiated for supported system types.
    nixpkgsFor = forAllSystems (system: import nixpkgs {inherit system;});
  in {
    # Provide some binary packages for selected system types.
    packages = forAllSystems (system: let
      pkgs = nixpkgsFor.${system};
      lib = nixpkgs.lib;

      ui = pkgs.stdenv.mkDerivation {
        name = "frontend";
        src = ./frontend;

        offlineCache = pkgs.fetchYarnDeps {
          yarnLock = ./frontend/yarn.lock;
          hash = "";
        };

        nativebuildInputs = with pkgs; [
          pkgs.yarnConfigHook
          pkgs.yarnBuildHook
          nodejs
          npmHooks.npmInstallHook
        ];
      };
    in {
      hub = pkgs.buildGoModule {
        pname = "hub";
        inherit version;
        src = ./.;

        vendorHash = "sha256-/yMMOMN+o7waqNgkHhEIMlDfsbF5X2OYoADITWQ28/E=";

        postPatch = ''
                 mkdir -p frontend/dist
          cp -r ${ui} ./frontend/dist
        '';
      };
    });

    # Add dependencies that are only needed for development
    devShells = forAllSystems (system: let
      pkgs = nixpkgsFor.${system};
    in {
      default = pkgs.mkShell {
        buildInputs = with pkgs; [go gopls gotools go-tools];
      };
    });

    # The default package for 'nix build'. This makes sense if the
    # flake provides only one package or there is a clear "main"
    # package.
    defaultPackage = forAllSystems (system: self.packages.${system}.hub);
  };
}
