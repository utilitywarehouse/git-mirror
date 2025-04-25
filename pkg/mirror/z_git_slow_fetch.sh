#!/bin/sh

if [ "$1" = "fetch" ]; then
  sleep 20
  git "$@"
  exit $?
fi

git "$@"