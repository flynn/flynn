#!/bin/bash
#
# Flynn autocomplete script for Bash.

_flynn_commands() {
  flynn help commands | cut -f 2 -d ' '
}

_flynn() {
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}

  if [ $COMP_CWORD -eq 1 ]; then
    COMPREPLY=( $( compgen -W "$(_flynn_commands)" ${cur} ) )
  elif [ $COMP_CWORD -eq 2 ]; then
    case "${prev}" in
      help) COMPREPLY=( $( compgen -W "$(_flynn_commands)" ${cur} ) ) ;;
    esac
  fi
}

complete -F _flynn -o default flynn
