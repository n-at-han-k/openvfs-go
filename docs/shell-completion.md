# Shell completion

Generate a completion script for Bash, Zsh, or Fish:

```sh
gob completion --shell bash
gob completion --shell zsh
gob completion --shell fish
```

Source-file arguments complete `.gob` files rather than `.go` files. Output
options such as `--output` still accept any path.

### Bash

Add the following to `~/.bashrc`:

```sh
eval "$(gob completion --shell bash)"
```

### Zsh

```sh
mkdir -p ~/.zsh/completions
gob completion --shell zsh > ~/.zsh/completions/_gob
```

Add the following to `~/.zshrc`, before any existing `compinit` call:

```sh
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit
compinit
```

### Fish

```sh
mkdir -p ~/.config/fish/completions
gob completion --shell fish > ~/.config/fish/completions/gob.fish
```
