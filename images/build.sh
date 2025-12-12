#!/usr/bin/env bash
set -euo pipefail

for file in *.Dockerfile; do
  [[ -e "$file" ]] || continue # no matches found
  echo "Building image from $file"
  docker build -f "$file" -t "codecell/${file%.Dockerfile}:latest" .
done
