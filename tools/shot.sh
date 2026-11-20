#!/bin/bash
# usage: shot.sh <session> <out.png>  — capture a tmux pane to PNG
set -e
mkdir -p /tmp/cap
tmux -L shots capture-pane -t "$1" -e -p > /tmp/cap/$1.ans
python3 "$(dirname "$0")/ansi2png.py" /tmp/cap/$1.ans "$2" ${3:-140} ${4:-40}
