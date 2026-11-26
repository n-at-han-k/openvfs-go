# Nix setup for Gob

## What the flake provides

Gob needs Ruby 4.0, Go 1.27, Bundler gems, and a C shared library for the Go formatter. `flake.nix` keeps those pieces in one reproducible development shell.

The repository flake supplies:

- Ruby 4.0 and its Bundler environment
- Go 1.27 and `gopls`
- `ast-grep`, `go-task`, `bundix`, Git, libyaml, OpenSSL, and pkg-config
- `BUNDLE_GEMFILE`, `RUBYLIB`, and `PATH` for this checkout

## Use the existing flake

```sh
nix develop
```

With `direnv`, the included `.envrc` loads the same shell:

```sh
direnv allow
```

Inside the shell:

```sh
bin/test
gob vet path/to/example.gob
gob transpile path/to/example.gob -o /tmp/example.go
```

## Set up or rebuild the flake

### 1. Enable flakes

Use Nix with flake support enabled:

```nix
# ~/.config/nix/nix.conf
experimental-features = nix-command flakes
```

### 2. Define the development shell

A Gob flake needs `nixpkgs`, `flake-utils`, Ruby 4.0, Go 1.27, and a `bundlerEnv` built from the lockfile and `gemset.nix`.

Minimal shape:

```nix
{
  description = "gob development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nix-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        ruby = pkgs.ruby_4_0;
        go = pkgs.go_1_27;
        gemset = import ./gemset.nix;
        gems = pkgs.bundlerEnv {
          name = "gob-gems";
          inherit ruby gemset;
          gemfile = ./Gemfile;
          lockfile = ./Gemfile.lock;
        };
      in
      {
        devShells.default = pkgs.mkShell {
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [
            gems
            gems.wrappedRuby
            go
            pkgs.gopls
            pkgs.git
            pkgs.libyaml
            pkgs.openssl
          ];

          shellHook = ''
            export BUNDLE_GEMFILE="$PWD/Gemfile"
            export BUNDLE_FROZEN=false
            export RUBYLIB="$PWD/lib''${RUBYLIB:+:$RUBYLIB}"
            export PATH="$PWD/exe''${PATH:+:$PATH}"
          '';
        };
      });
}
```

Use the repository's full `flake.nix` as the reference for `ast-grep`, `go-task`, `bundix`, and platform-specific gem hashes.

### 3. Generate the Nix gem set

`gemset.nix` is generated from `Gemfile.lock`; do not hand-maintain it.

After changing gems:

```sh
nix develop -c bundle lock
nix develop -c bundix -l
```

The repository overrides Bundix's Bundler with Ruby 4.0. Without that override, Bundix may resolve against Nix's default Ruby and reject gems requiring Ruby 4.0.

The shell hook also runs `bundix -l` when `gemset.nix` is writable.

### 4. Build the native Go formatter

`lib/go_formatter.rb` loads `ext/goformatter/libgoformatter.so` on Linux or `.dylib` on macOS.

```sh
nix develop
cd ext/goformatter
CGO_ENABLED=1 go build \
  -buildmode=c-shared \
  -buildvcs=false \
  -o libgoformatter.so .
```

Use `-o libgoformatter.dylib` on macOS. The formatter module requires Go 1.27, which is why the pinned Go toolchain matters.

## Common failures

- **`cannot load such file ...`**: enter `nix develop` and use the checkout's `bundle exec ruby exe/gob ...`; system Ruby does not have the bundled gems.
- **`libgoformatter` load error**: rebuild the C shared library in `ext/goformatter`.
- **Ruby version or lockfile errors**: use the flake's Ruby 4.0 and regenerate `gemset.nix` with the overridden Bundix.
- **Go 1.27 syntax or API errors**: use the flake's Go 1.27, not an older system Go.
- **Stale dependency hashes**: rerun `bundix -l`; FFI and tree-sitter gems may need platform-specific hash overlays like those in the repository flake.
