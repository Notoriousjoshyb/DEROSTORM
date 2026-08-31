#!/usr/bin/env bash
#
# Starts the miner. HiveOS runs this under screen and reads the log it writes.

cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1
. h-manifest.conf
[[ -f "$CUSTOM_CONFIG_FILENAME" ]] && . "$CUSTOM_CONFIG_FILENAME"

mkdir -p "$(dirname "$CUSTOM_LOG_BASENAME")"

# The stats file is how h-stats.sh learns anything. It lives beside the miner
# rather than in /tmp, so two rig managers on one box cannot collide over it,
# and the miner deletes it on the way out -- which is what lets h-stats.sh tell
# "stopped" from "running at zero".
STATS="$(pwd)/derostorm-stats.json"

# --no-tui because there is no terminal here: the full-screen console would
# write cursor control into a log file. The miner still keeps its own
# derostorm.log beside the binary; this is the one HiveOS shows.
#
# The miner is backgrounded rather than exec'd, because `exec cmd | tee` does
# not do what it looks like -- a pipeline is forked, so exec replaces nothing.
#
# And the log is teed through a process substitution rather than a pipe, which
# is the part that matters: in `cmd | tee &` the shell's $! is tee's pid, not
# the miner's, so a trap that signals $! signals the wrong process and the
# miner is killed later and harder, without running its own shutdown. With
# `> >(tee ...)` the miner is the direct child and $! is really the miner.
#
# It gets a TERM, which it handles: it stops mining and removes the stats file,
# so HiveOS sees the rig stop at once instead of waiting out h-stats.sh's
# sixty-second staleness window.
./derostorm   --no-tui   --daemon-rpc-address="$DEROSTORM_NODE"   --wallet-address="$DEROSTORM_WALLET"   --stats-file="$STATS"   $DEROSTORM_EXTRA   > >(tee -a "$CUSTOM_LOG_BASENAME.log") 2>&1 &

miner=$!
trap 'kill -TERM $miner 2>/dev/null' TERM INT HUP
wait $miner
exit $?
