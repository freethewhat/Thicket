# Source this file from ~/.zshrc:
#   source /path/to/thicket.zsh
th() {
  local dir
  dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"
}
