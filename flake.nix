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
            sha256 = "1pw0q28nvllqysx7g9gv6vy2gz6csbiwm8rx37hzbn7zj5pz025j";
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
            sha256 = "0swv0pa2h7d4i4v306wz51qm8y36q70bfzx8xzd8gva972jn8cyl";
          };
          aarch64-linux = {
            platform = "aarch64-linux";
            sha256 = "1wramigwc6jkhjk6blq3mrnz535pfmn2108f1vwg8r4g1fi4r3k7";
          };
          aarch64-darwin = {
            platform = "arm64-darwin";
            sha256 = "1snw4c6ny41nj6aqlblw6s8qsab8bsm3v55spkky3k5r8ynkkiyj";
          };
          x86_64-darwin = {
            platform = "x86_64-darwin";
            sha256 = "1v2ihf0f0hw5dzzqiziiy2s3r0q2l2xbndjmaaair69ar2slxrwy";
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
            sqlite
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
