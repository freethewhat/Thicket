# Source this file from ~/.zshrc:
#   source /path/to/thicket.zsh
thicket() {
  case "$1" in
    -h|--help)
      command thicket-bin "$@"
      return
      ;;
  esac
  local dir
  dir=$(command thicket-bin "$@") && [ -n "$dir" ] && cd -- "$dir"
}
