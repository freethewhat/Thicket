# Source this file from ~/.bashrc:
#   source /path/to/thicket.bash
th() {
  local dir
  dir=$(command thicket "$@") && [ -n "$dir" ] && cd -- "$dir"
}
