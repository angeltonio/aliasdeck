# Source this file from zsh. It intentionally does not support execution:
# activation must define `aliasdeck` in the current shell so a successful sync
# can load the generated aliases into that same shell.
if [[ -z ${ZSH_VERSION:-} ]]; then
  print -u2 'AliasDeck development activation requires zsh: source scripts/dev.zsh'
  return 1 2>/dev/null || exit 1
fi

typeset -g ALIASDECK_DEV_ROOT=${${(%):-%N}:A:h:h}
typeset -gx ALIASDECK_HOME="$ALIASDECK_DEV_ROOT/build/aliasdeck-dev/client"
if (( ! ${+ALIASDECK_DEV_LOADED_ALIASES} )); then
  typeset -g -a ALIASDECK_DEV_LOADED_ALIASES
  ALIASDECK_DEV_LOADED_ALIASES=()
fi

aliasdeck() {
  emulate -L zsh

  local binary="$ALIASDECK_DEV_ROOT/build/aliasdeck-dev/bin/aliasdeck"
  mkdir -p "${binary:h}" || return
  (cd "$ALIASDECK_DEV_ROOT" && go build -o "$binary" ./cmd/aliasdeck) || return

  command "$binary" "$@"
  local exit_code=$?
  if (( exit_code != 0 )) || [[ ${1:-} != sync ]]; then
    return $exit_code
  fi

  local generated="$ALIASDECK_HOME/aliases.zsh"
  if [[ ! -r $generated ]]; then
    print -u2 "AliasDeck synced successfully but $generated is not readable"
    return 1
  fi

  local name
  for name in "${ALIASDECK_DEV_LOADED_ALIASES[@]}"; do
    unalias -- "$name" 2>/dev/null || true
  done

  local -a loaded
  local line
  while IFS= read -r line; do
    [[ $line == 'alias '*=* ]] || continue
    name=${line#alias }
    name=${name%%=*}
    [[ $name =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || continue
    loaded+=("$name")
  done < "$generated"

  source "$generated" || return
  ALIASDECK_DEV_LOADED_ALIASES=("${loaded[@]}")
  print "Loaded ${#loaded} AliasDeck alias(es) into the current zsh."
}

aliasdeck_dev_deactivate() {
  emulate -L zsh

  local name
  for name in "${ALIASDECK_DEV_LOADED_ALIASES[@]}"; do
    unalias -- "$name" 2>/dev/null || true
  done
  unset ALIASDECK_DEV_LOADED_ALIASES ALIASDECK_HOME ALIASDECK_DEV_ROOT
  unfunction aliasdeck aliasdeck_dev_deactivate
}

print "AliasDeck development shell active. Client state: $ALIASDECK_HOME"
