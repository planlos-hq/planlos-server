#!/usr/bin/env bash
find . -path ./db -prune -o -name '*.sql' -print0 | xargs -0 pg_format -i
