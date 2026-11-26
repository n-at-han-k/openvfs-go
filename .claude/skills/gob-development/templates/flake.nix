# Copy to flake.nix. Keep the ffi platform hashes aligned with Gemfile.lock.
{
  description = "gob | a ruby -> go transpiler";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, utils }:
    utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        ruby = pkgs.ruby_4_0;
        go = pkgs.go_1_27;

        # Bundix must resolve with the same Ruby as the development shell.
        bundix = pkgs.bundix.override {
          bundler = pkgs.bundler.override { inherit ruby; };
        };

        gemset = import ./gemset.nix;

        ffiPack = {
          x86_64-linux = { platform = "x86_64-linux-gnu"; sha256 = "0bbmph141hwnn3m4k8hx8nhy1imfjpz87h595ww4n1xf5r6b2gcx"; };
          aarch64-linux = { platform = "aarch64-linux-gnu"; sha256 = "1pw0q28vnllqysx7g9gv6vy2gz6csbiwm8rx37hzbn7zj5pz025j"; };
          aarch64-darwin = { platform = "arm64-darwin"; sha256 = "06q06ylk32yma4ms84sd6cxhlcvp1sbbyal58q51n98r2jpil1qr"; };
          x86_64-darwin = { platform = "x86_64-darwin"; sha256 = "15m4rlngxc2ff6pb4b1a11fbvkxl16vn4jgndh4kacng4c2kjw5a"; };
        }.${system};

        gems = pkgs.bundlerEnv {
          name = "gob-gems";
          inherit ruby;
          gemfile = ./Gemfile;
          lockfile = ./Gemfile.lock;
          groups = [ "default" "development" ];
          gemset = gemset
            // pkgs.lib.optionalAttrs (gemset ? ffi) {
              ffi = gemset.ffi // {
                inherit (ffiPack) platform;
                source = gemset.ffi.source // { inherit (ffiPack) sha256; };
              };
            };
        };

        ast-grep = pkgs.runCommand "ast-grep-0.51.3" { } ''
          install -Dm755 ${pkgs.fetchurl {
            url = "https://github.com/n-at-han-k/rust-builds/releases/download/ast-grep-v0.51.3/ast-grep";
            hash = "sha256-PIuXm5sDJQMnTgQ5xqqsa6Kffo9+gcCM6evFcakPViw=";
          }} $out/bin/ast-grep
        '';

      in
      {
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = [ pkgs.pkg-config ];

          buildInputs = [
            gems
            gems.wrappedRuby

            ast-grep
            bundix
            pkgs.go-task

            go
            pkgs.gopls

            pkgs.git
            pkgs.libyaml
            pkgs.openssl
          ];

          shellHook = /* bash */ ''
            export BUNDLE_GEMFILE="$PWD/Gemfile"
            export BUNDLE_FROZEN=false
            export RUBYLIB="$PWD/lib''${RUBYLIB:+:$RUBYLIB}"
            export PATH="$PWD/exe''${PATH:+:$PATH}"

            if [ -w gemset.nix ]; then bundix -l; fi
          '';
        };
      });
}
