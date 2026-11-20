#!/bin/bash
# usage: run.sh <session> <cols> <rows> <command...> — start a headless tmux session
s=$1; w=$2; h=$3; shift 3
mkdir -p /tmp/cap
if [ ! -f /tmp/cap/tmux.conf ]; then
  cat > /tmp/cap/tmux.conf <<'CONF'
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*:Tc"
set -g status off
set -g escape-time 0
CONF
fi
tmux -L shots -f /tmp/cap/tmux.conf kill-session -t $s 2>/dev/null || true
tmux -L shots -f /tmp/cap/tmux.conf new-session -d -s $s -x $w -y $h "env COLORTERM=truecolor TERMAIL_TRUECOLOR=1 $*"
