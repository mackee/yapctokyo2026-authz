#!/bin/zsh
# Run trials from a queue file with bounded concurrency.
# usage: run-queue.sh <queue-file> [max-parallel]
# queue lines: <trial-id> <base-repo> <task-file>
Q=$1; MAX=${2:-6}; S=${0:A:h}; L=${CONTROL:-$HOME/src/haiku-probe-control}/logs
mkdir -p $L
while read -r id base task; do
  [[ -z $id || $id == \#* ]] && continue
  while (( $(pgrep -f "run-trial.sh" | wc -l) >= MAX )); do sleep 10; done
  BASE_REPO=$base $S/run-trial.sh $id $task > $L/$id.driver.out 2>&1 &
  sleep 12
done < $Q
wait
