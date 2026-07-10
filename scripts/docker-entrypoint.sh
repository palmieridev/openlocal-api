#!/bin/sh
set -eu

case "${RUN_MIGRATIONS:-true}" in
	true|1|yes)
		if [ -z "${DATABASE_URL:-}" ]; then
			echo "RUN_MIGRATIONS=true but DATABASE_URL is not set" >&2
			exit 1
		fi
		echo "running database migrations"
		migrate -path "${MIGRATIONS_PATH:-/app/db/migrations}" -database "$DATABASE_URL" up
		;;
	false|0|no)
		echo "skipping database migrations"
		;;
	*)
		echo "RUN_MIGRATIONS must be true or false" >&2
		exit 1
		;;
esac

exec "$@"
