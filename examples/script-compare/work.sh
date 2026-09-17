#!/bin/sh
# A stand-in for a CLI written as a script: it counts the records on
# standard input.
count=0
while IFS= read -r line; do
  count=$((count + 1))
done
echo "$count"
