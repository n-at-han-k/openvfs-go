{
  description = "openvfs";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        gemset = import ./gemset.nix;

        ffiPack = {
          x86_64-linux = {
            platform = "x86_64-linux-gnu";
            sha256 = "0bbmph141hwnn3m4k8hx8nhy1imfjpz87h595ww4n1xf5r6b2gcx";
          };
          aarch64-linux = {
            platform = "aarch64-linux-gnu";
            sha256 = "1pw0q28vnllqysx7g9gv6vy2gz6csbiwm8rx37hzbn7zj5pz025j";
          };
          aarch64-darwin = {
            platform = "arm64-darwin";
            sha256 = "06q06ylk32yma4ms84sd6cxhlcvp1sbbyal58q51n98r2jpil1qr";
          };
          x86_64-darwin = {
            platform = "x86_64-darwin";
            sha256 = "15m4rlngxc2ff6pb4b1a11fbvkxl16vn4jgndh4kacng4c2kjw5a";
          };
        }.${system};

        gobPack = {
          x86_64-linux = {
            platform = "x86_64-linux";
            sha256 = "1jsjzfma7sxl7k911a9h26lzxy906av1x0704kwfx998yikrym54";
          };
          aarch64-linux = {
            platform = "aarch64-linux";
            sha256 = "0kyxkg5hmnq7hvrh0hfpar99ffz9dy1z42h27jllaprwn41gg8ii";
          };
          aarch64-darwin = {
            platform = "arm64-darwin";
            sha256 = "0jyin7f0845v0jhlg8iknasd57g40b1gpbf9d83mmn7nzhhysh6k";
          };
          x86_64-darwin = {
            platform = "x86_64-darwin";
            sha256 = "0wlrscw37f0nz29ygnbkq7fkb6qzp8grlivnfrzi1hkh65bd8xi8";
          };
        }.${system};

        gems = pkgs.bundlerEnv {
          name = "gob-gems";
          ruby = pkgs.ruby_4_0;
          gemfile = ./Gemfile;
          lockfile = ./Gemfile.lock;
          groups = [ "default" "development" ];
          gemset = pkgs.lib.recursiveUpdate gemset {
            ffi.platform = ffiPack.platform;
            ffi.source.sha256 = ffiPack.sha256;

            gob-lang.platform = gobPack.platform;
            gob-lang.source.sha256 = gobPack.sha256;
          };
        };
      in
      {
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            pkg-config
          ];

          buildInputs = with pkgs; [
            gems
            gems.wrappedRuby

            go_1_27
            gopls
            gotools

            fuse3
            libxml2
            bat
          ];

          shellHook = ''
            export GEM_PATH="${gems}/${pkgs.ruby_4_0.gemPath}"''${GEM_PATH:+:$GEM_PATH}
            export BUNDLE_FORCE_RUBY_PLATFORM=false
            export BUNDLE_GEMFILE="$PWD/Gemfile"
            export BUNDLE_FROZEN=false
            export PATH="$PWD/bin:$PWD/hack:$PATH"
          '';
        };
      });
}
