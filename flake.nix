{
  description = "Alby Hub";

  inputs.nixpkgs.url = "nixpkgs/nixos-24.05";

  outputs = {
    self,
    nixpkgs,
  }: let
    version = "1.6.0";

    supportedSystems = ["x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin"];

    # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
    forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

    # Nixpkgs instantiated for supported system types.
    nixpkgsFor = forAllSystems (system: import nixpkgs {inherit system;});

    # bindings repo URLs
    breezSdkRepoUrl = "https://github.com/breez/breez-sdk-go";
    glalbyGoRepoUrl = "https://github.com/getAlby/glalby-go";
    ldkNodeGoRepoUrl = "https://github.com/getAlby/ldk-node-go";
  in {
    packages = forAllSystems (system: let
      pkgs = nixpkgsFor.${system};
      lib = pkgs.lib;
      arch = builtins.head (lib.strings.splitString "-" system );

      breez-sdk = pkgs.fetchFromGitHub {
             repo = "breez-sdk-go";
	     owner = "breez";
	     rev = "v0.5.2";
             hash = "sha256-zE8GIMi/YopprNVT3rAGqykosCtuKKnVHBSObdp/ONo=";
	  };

      glalby-go = pkgs.fetchFromGitHub {
             repo = "glalby-go";
	     owner = "getAlby";
	     rev = "95673c864d5954a6f78a24600876784b81af799f";
             hash = "sha256-DwYOJkk0VzCMzHMUM+C31Kq8e6jIsBZNBQLgpylOe4E=";
	  };

      ldk-node-go = pkgs.fetchFromGitHub {
         repo = "ldk-node-go";
	 owner = "getAlby";
	 rev = "6fa575b0a3f57fdb8af0d1efd1ed895bb3144ce7";
	 hash = "sha256-UsCBUBulu2KrvByJwzOAbIW5DP9f1JQw/PVnO5o+gog=";
      };

      breez-bindings = if arch == "aarch" then "${breez-sdk}/breez_sdk/lib/darwin-aarch64/libbreez_sdk_bindings.dylib"
         else "${breez-sdk}/breez_sdk/lib/linux-amd64/libbreez_sdk_bindings.so";

      ldk-node-bindings = if arch == "aarch" then "${ldk-node-go}/ldk_node/aarch64-unknown-linux-gnu/libldk_node.so"
         else "${ldk-node-go}/ldk_node/x86_64-unknown-linux-gnu/libldk_node.so";

      glalby-bindings = if arch == "aarch" then "${glalby-go}/glalby/aarch64-unknown-linux-gnu/libglalby_bindings.so"
         else "${glalby-go}/glalby/x86_64-unknown-linux-gnu/libglalby_bindings.so";

      ui = pkgs.stdenv.mkDerivation {
        name = "frontend";
        src = ./frontend;

        offlineCache = pkgs.fetchYarnDeps {
          yarnLock = ./frontend/yarn.lock;
          hash = "sha256-IkjEwqlPuYp3RzitpgbWeqSlSxDuEAgeK9p4reUkI5Y=";
        };

        nativebuildInputs = with pkgs; [
          nodejs
          npmHooks.npmInstallHook
        ];

        installPhase = ''
          runHook preInstall
          cp -r . $out
          runHook postInstall
        '';
      };
    in {
      hub = pkgs.buildGoModule {
        pname = "hub";
        inherit version;
        src = ./.;

        vendorHash = "sha256-/yMMOMN+o7waqNgkHhEIMlDfsbF5X2OYoADITWQ28/E=";

        buildInputs = [pkgs.patchelf pkgs.stdenv.cc.cc.lib];

        CGO_ENABLED = 1;
        postPatch = ''
          mkdir -p frontend/dist
          cp -r ${ui} ./frontend/dist
        '';

        preConfigure = ''
	  export BINDING_OUTDIR=$out/lib/nwc
          mkdir -p $BINDING_OUTDIR
	  export LD_LIBRARY_PATH=$BINDING_OUTDIR:$LD_LIBRARY_PATH
	  cp ${breez-bindings} $BINDING_OUTDIR/libbreez_sdk_bindings.so
	  cp ${glalby-bindings} $BINDING_OUTDIR/libglalby_bindings.so
	  cp ${ldk-node-bindings} $BINDING_OUTDIR/libldk_node.so
	  export LDFLAGS="-L$BINDING_OUTDIR -lldk_node -lglalby_bindings -lstdc++"
	  export CGO_LDFLAGS="$LDFLAGS"
	'';

	buildPhase = ''
          runHook preBuild
          go build -o $out/bin/http-server cmd/http/main.go
	  runHook postBuild
	'';

    postInstall = ''
      patchelf --set-rpath $out/lib/nwc:${pkgs.stdenv.cc.cc.lib}/lib $out/bin/http-server
      ldd $out/bin/http-server
      patchelf --print-rpath $out/bin/http-server
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
