#!/usr/bin/env bash
set -euo pipefail

: "${DATABASE_URL:=postgres://openlocal:openlocal@localhost:5432/openlocal?sslmode=disable}"
migrate -path db/migrations -database "$DATABASE_URL" up
