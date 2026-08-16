# Source this file from ~/.bashrc:
#   source /path/to/thicket.bash
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
