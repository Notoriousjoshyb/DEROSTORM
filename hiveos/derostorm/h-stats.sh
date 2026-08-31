#!/usr/bin/env bash
#
# Reports to HiveOS. Sourced, not executed: it sets khs and stats and returns.
#
# Everything comes from the JSON document the miner writes every five seconds
# (--stats-file), not from the log. Parsing a log for a hashrate means parsing a
# console that was designed for a person, and it breaks the first time a column
# is widened.
#
# Because this is sourced, $0 is whatever sourced it and not this file, and a
# `cd` here would move the *caller's* working directory. Both of those are easy
# to get wrong and neither fails loudly, so paths come from BASH_SOURCE and
# nothing changes directory.

_ds_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$_ds_dir/h-manifest.conf" ]] && . "$_ds_dir/h-manifest.conf"

_ds_file="$_ds_dir/derostorm-stats.json"

khs=0
stats=

if [[ -f "$_ds_file" ]]; then
  # A document older than a minute means the miner has stopped writing. Report
  # nothing rather than the last hashrate it managed: a frozen number looks
  # like a working rig, which is the one thing a monitor must never show.
  _ds_now=$(date +%s)
  _ds_mtime=$(stat -c %Y "$_ds_file" 2>/dev/null || echo 0)

  if (( _ds_now - _ds_mtime <= 60 )) && jq -e . "$_ds_file" >/dev/null 2>&1; then

    # khs is the rig total and includes the CPU, because DeroStorm mines on
    # both and a total that left the CPU out would not match the miner's own
    # console. The hs array below is GPUs only, which is what HiveOS draws per
    # card, so on a rig that is also mining on its processor the array does not
    # sum to the total. The difference is the CPU.
    khs=$(jq -r '(.hashrate // 0) / 1000 | .*1000 | round / 1000' "$_ds_file")

    _ds_uptime=$(jq -r '.uptime // 0'    "$_ds_file")
    _ds_ver=$(jq    -r '.version // "?"' "$_ds_file")

    # Solo mining, so "accepted" is miniblocks -- a share of a block -- plus
    # full blocks. There is no pool to accept anything.
    _ds_acc=$(jq -r '(.miniblocks // 0) + (.blocks // 0)' "$_ds_file")
    _ds_rej=$(jq -r '.rejected // 0'                      "$_ds_file")

    # Per-card arrays, in the miner's device order, which is CUDA ordinal order.
    _ds_hs=$(jq   -c '[.devices[] | select(.is_gpu) | ((.hashrate // 0) / 1000)]'   "$_ds_file")
    _ds_temp=$(jq -c '[.devices[] | select(.is_gpu) | ((.temp_c  // 0) | floor)]'   "$_ds_file")
    _ds_fan=$(jq  -c '[.devices[] | select(.is_gpu) | (.fan_pct // 0)]'             "$_ds_file")

    # Bus numbers let HiveOS line those arrays up with the cards it knows about.
    # CUDA ordinal order is not always HiveOS's order, and without this a
    # two-card rig can show each card's hashrate against the other one.
    _ds_bus=$(nvidia-smi --query-gpu=pci.bus_id --format=csv,noheader 2>/dev/null |
              awk -F: '{ printf "%d\n", strtonum("0x" $(NF-1)) }' |
              jq -cRs 'split("\n") | map(select(length > 0) | tonumber)' 2>/dev/null)
    [[ -z "$_ds_bus" || "$_ds_bus" == "null" ]] && _ds_bus="[]"

    # One per card or none at all. A mismatched array is worse than a missing
    # one: HiveOS lines the per-card figures up by it, so the wrong length
    # shows each card's hashrate against a different card. The lengths differ
    # whenever --gpu names a subset, because nvidia-smi lists every card in the
    # rig and the miner is only reporting the ones it is using.
    if [[ "$(jq 'length' <<< "$_ds_bus")" != "$(jq 'length' <<< "$_ds_hs")" ]]; then
      _ds_bus="[]"
    fi

    stats=$(jq -nc \
      --argjson hs     "$_ds_hs" \
      --argjson temp   "$_ds_temp" \
      --argjson fan    "$_ds_fan" \
      --argjson bus    "$_ds_bus" \
      --argjson uptime "$_ds_uptime" \
      --argjson acc    "$_ds_acc" \
      --argjson rej    "$_ds_rej" \
      --arg     ver    "$_ds_ver" \
      '{
         hs: $hs, hs_units: "khs",
         temp: $temp, fan: $fan,
         bus_numbers: $bus,
         uptime: $uptime,
         ar: [$acc, $rej],
         algo: "astrobwtv3",
         ver: $ver
       }')
  fi
fi

unset _ds_dir _ds_file _ds_now _ds_mtime _ds_uptime _ds_ver _ds_acc _ds_rej
unset _ds_hs _ds_temp _ds_fan _ds_bus
