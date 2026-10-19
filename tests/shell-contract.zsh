#!/usr/bin/env zsh
emulate -L zsh
setopt errexit nounset pipefail
project=${0:A:h:h}
fixture=$(mktemp -d)
trap 'command rm -rf -- "$fixture"' EXIT
command mkdir "$fixture/bin"
export ZSH_GIT_INLAY_HELPER_MARKER="$fixture/helper-launched"
cat > "$fixture/bin/zsh-git-inlay" <<'SH'
#!/bin/sh
: > "$ZSH_GIT_INLAY_HELPER_MARKER"
exit 99
SH
command chmod 755 "$fixture/bin/zsh-git-inlay"
path=("$fixture/bin" $path)
source "${ZSH_AUTOSUGGESTIONS_SOURCE:?}"
typeset -g ZSH_GIT_INLAY_CYCLE_KEYBINDING='^Xg'
typeset -ga ZSH_GIT_INLAY_CYCLE_KEYMAPS=(emacs viins)
before_emacs=$(bindkey -M emacs '^Xg')
before_vi=$(bindkey -M viins '^Xg')
source "$project/zsh-git-inlay.plugin.zsh"
source "$project/zsh-git-inlay.plugin.zsh"
[[ ${#${(M)ZSH_AUTOSUGGEST_STRATEGY:#git-inlay}} == 1 ]]
# An executable fixture catches `command` invocations, which bypass functions.
for buffer in 'echo hi' 'ls' 'gitstatus' 'g' ''; do
  suggestion=''
  _zsh_autosuggest_strategy_git-inlay "$buffer"
  [[ -z $suggestion ]]
done
[[ ! -e $ZSH_GIT_INLAY_HELPER_MARKER ]] || { print -u2 -- 'unrelated typing launched the helper'; exit 1 }
# Positive control: prove the fixture observes a supported command's launch.
_zsh_autosuggest_strategy_git-inlay 'git commit -m '
[[ -e $ZSH_GIT_INLAY_HELPER_MARKER ]] || { print -u2 -- 'helper fixture did not observe a supported fetch'; exit 1 }
for buffer in 'git commit -m ' $' \tgit\tcommit -m ' 'command git commit -m ' 'git --no-pager commit -m '; do
  _zsh_git_inlay_maybe_commit "$buffer"
done
[[ $(bindkey -M emacs '^Xg') == *zsh-git-inlay-cycle ]]
[[ $(bindkey -M viins '^Xg') == *zsh-git-inlay-cycle ]]
zsh_git_inlay_unload
[[ $(bindkey -M emacs '^Xg') == "$before_emacs" ]]
[[ $(bindkey -M viins '^Xg') == "$before_vi" ]]
# Reload and disabled cycling must also unload cleanly.
ZSH_GIT_INLAY_CYCLE_KEYBINDING=none
source "$project/zsh-git-inlay.plugin.zsh"
zsh_git_inlay_unload
print 'shell contract: ok'
