#!/bin/sh
# Used by apps/docker-compose.prod.yml.
set -eu

: "${POSTGRES_HOST:?POSTGRES_HOST is required}"
: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${BACKUP_INTERVAL_SECONDS:?BACKUP_INTERVAL_SECONDS is required}"
: "${BACKUP_RETENTION_DAYS:?BACKUP_RETENTION_DAYS is required}"

case "$BACKUP_INTERVAL_SECONDS" in
	''|*[!0-9]*) echo "BACKUP_INTERVAL_SECONDS must be numeric" >&2; exit 1 ;;
esac
case "$BACKUP_RETENTION_DAYS" in
	''|*[!0-9]*) echo "BACKUP_RETENTION_DAYS must be numeric" >&2; exit 1 ;;
esac

umask 077
mkdir -p /backups
export PGPASSWORD="$POSTGRES_PASSWORD"

backup_once() {
	timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
	final="/backups/frux-$timestamp.dump"
	temp="/backups/.frux-$timestamp.dump.tmp"
	marker="/backups/.last-success.tmp"

	rm -f "$temp" "$marker"
	if ! pg_dump \
		--host "$POSTGRES_HOST" \
		--username "$POSTGRES_USER" \
		--dbname "$POSTGRES_DB" \
		--format custom \
		--compress 9 \
		--file "$temp"; then
		rm -f "$temp"
		return 1
	fi
	if [ ! -s "$temp" ]; then
		rm -f "$temp"
		return 1
	fi
	if ! mv "$temp" "$final"; then
		rm -f "$temp"
		return 1
	fi
	if ! date +%s >"$marker"; then
		return 1
	fi
	if ! mv "$marker" /backups/.last-success; then
		rm -f "$marker"
		return 1
	fi
	if ! find /backups -type f -name 'frux-*.dump' -mtime "+$BACKUP_RETENTION_DAYS" -delete; then
		return 1
	fi
	echo "PostgreSQL backup completed: $(basename "$final")"
}

backup_once
if [ "${BACKUP_ONCE:-false}" = "true" ]; then
	exit 0
fi
while sleep "$BACKUP_INTERVAL_SECONDS"; do
	if ! backup_once; then
		rm -f /backups/.last-success
		echo "PostgreSQL backup failed" >&2
	fi
done
