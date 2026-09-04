#!/usr/bin/env bash
# Host-side HAProxy reload after proxyctl writes /etc/haproxy/haproxy.cfg.
# Pair with deploy/systemd/proxyctl-haproxy-reload.path. The live Docker agent
# never calls systemctl; this script is the only reload path.
#
# Protocol: flock → debounce until the file hash is stable → skip if hash
# matches last successful reload → haproxy -c → snapshot last-good →
# systemctl reload → persist hash. Validation or reload failure restores
# last-good onto the live path and does not leave HAProxy on a bad config.
set -euo pipefail

readonly CFG=/etc/haproxy/haproxy.cfg
readonly UNIT=haproxy
readonly LOCK=/run/proxyctl/haproxy-reload.lock
readonly DEBOUNCE_STATE=/run/proxyctl/haproxy-reload.debounce
readonly STATE=/var/lib/proxyctl/haproxy-reload.state
readonly LAST_GOOD=/var/lib/proxyctl/haproxy.last-good.cfg
readonly BACKUP_DIR=/var/lib/proxyctl/haproxy-backups
readonly SETTLE_SEC=2
readonly MAX_WAIT_SEC=12
readonly BACKUP_KEEP=20

log() {
	printf 'proxyctl-haproxy-reload: %s\n' "$*" >&2
}

haproxy_bin() {
	if [ -x /usr/sbin/haproxy ]; then
		printf '%s\n' /usr/sbin/haproxy
		return 0
	fi
	if [ -x /usr/bin/haproxy ]; then
		printf '%s\n' /usr/bin/haproxy
		return 0
	fi
	return 1
}

file_sha256() {
	local i out
	for i in 1 2 3 4 5 6 7 8 9 10; do
		if [ -f "$CFG" ]; then
			out=$(sha256sum -- "$CFG") || out=""
			if [ -n "$out" ]; then
				printf '%s\n' "${out%% *}"
				return 0
			fi
		fi
		sleep 0.1
	done
	return 1
}

read_state_hash() {
	local k v
	if [ ! -f "$STATE" ]; then
		return 0
	fi
	while IFS='=' read -r k v; do
		case "$k" in
		hash)
			case "$v" in
			*[!0-9a-fA-F]* | "") ;;
			*)
				printf '%s\n' "$v"
				return 0
				;;
			esac
			;;
		esac
	done <"$STATE"
	return 0
}

write_state() {
	local hash="$1"
	local tmp mtime
	tmp="${STATE}.tmp"
	mtime=$(stat -c %Y -- "$CFG")
	printf 'hash=%s\nreloaded_at=%s\ncfg_mtime=%s\n' "$hash" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$mtime" >"$tmp"
	mv -f -- "$tmp" "$STATE"
}

write_debounce_state() {
	local hash="$1"
	local tmp
	tmp="${DEBOUNCE_STATE}.tmp"
	printf 'hash=%s\nlast_write_at=%s\n' "$hash" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$tmp"
	mv -f -- "$tmp" "$DEBOUNCE_STATE"
}

prune_backups() {
	local f i=0
	local -a all=()
	shopt -s nullglob
	all=("$BACKUP_DIR"/haproxy-*.cfg)
	shopt -u nullglob
	if [ "${#all[@]}" -le "$BACKUP_KEEP" ]; then
		return 0
	fi
	while IFS= read -r f; do
		i=$((i + 1))
		if [ "$i" -gt "$BACKUP_KEEP" ]; then
			rm -f -- "$f"
		fi
	done < <(ls -1t -- "${all[@]}")
}

snapshot_last_good() {
	local dest
	if [ ! -f "$LAST_GOOD" ]; then
		return 0
	fi
	dest="${BACKUP_DIR}/haproxy-$(date -u +%Y%m%dT%H%M%SZ).cfg"
	cp -a -- "$LAST_GOOD" "$dest"
	log "event=backup_written path=${dest}"
	prune_backups
}

restore_last_good() {
	local tmp
	if [ ! -f "$LAST_GOOD" ]; then
		log "event=restore_on_failure_skipped reason=no_last_good"
		return 1
	fi
	log "event=restore_on_failure_triggered"
	tmp="${CFG}.proxyctl-restore"
	cp -a -- "$LAST_GOOD" "$tmp"
	mv -f -- "$tmp" "$CFG"
	log "event=restore_on_failure_done"
}

debounce() {
	local start now prev="" cur stable=0
	start=$(date +%s)
	log "event=debounce_window_started settle_sec=${SETTLE_SEC} max_sec=${MAX_WAIT_SEC}"
	while true; do
		cur=$(file_sha256) || {
			log "event=config_unreadable path=${CFG}"
			return 1
		}
		if [ "$cur" != "$prev" ]; then
			if [ -n "$prev" ]; then
				log "event=debounce_window_reset hash=${cur}"
			fi
			write_debounce_state "$cur"
			prev="$cur"
			stable=0
		else
			stable=$((stable + 1))
		fi
		if [ "$stable" -ge "$SETTLE_SEC" ]; then
			now=$(date +%s)
			log "event=debounce_window_settled hash=${cur} waited_sec=$((now - start))"
			return 0
		fi
		now=$(date +%s)
		if [ $((now - start)) -ge "$MAX_WAIT_SEC" ]; then
			log "event=debounce_window_max_wait hash=${cur}"
			return 0
		fi
		sleep 1
	done
}

run_validate() {
	local bin errf
	bin=$(haproxy_bin) || {
		log "event=validation_failed reason=haproxy_binary_missing"
		return 1
	}
	log "event=validation_started path=${CFG}"
	errf=$(mktemp)
	if ! "$bin" -c -f "$CFG" >"$errf" 2>&1; then
		log "event=validation_failed hash=${1}"
		while IFS= read -r line; do
			printf 'proxyctl-haproxy-reload: event=validation_output msg=%s\n' "$line" >&2
		done <"$errf"
		rm -f -- "$errf"
		return 1
	fi
	rm -f -- "$errf"
	log "event=validation_passed hash=${1}"
}

run_reload() {
	local errf
	if [ ! -x /usr/bin/systemctl ]; then
		log "event=reload_failed reason=systemctl_missing"
		return 1
	fi
	log "event=reload_started unit=${UNIT}"
	errf=$(mktemp)
	if ! /usr/bin/systemctl reload "$UNIT" >"$errf" 2>&1; then
		log "event=reload_failed unit=${UNIT}"
		while IFS= read -r line; do
			printf 'proxyctl-haproxy-reload: event=reload_output msg=%s\n' "$line" >&2
		done <"$errf"
		rm -f -- "$errf"
		return 1
	fi
	rm -f -- "$errf"
	log "event=reload_succeeded unit=${UNIT}"
}

mkdir -p -- /run/proxyctl /var/lib/proxyctl "$BACKUP_DIR"

exec 9>"$LOCK"
if ! flock -n 9; then
	log "event=lock_wait path=${LOCK}"
	flock 9
fi
log "event=lock_acquired path=${LOCK}"

if [ ! -f "$CFG" ]; then
	log "event=change_detected result=missing path=${CFG}"
	exit 1
fi

log "event=change_detected path=${CFG}"

while true; do
	debounce

	cur=$(file_sha256) || {
		log "event=config_unreadable path=${CFG}"
		exit 1
	}
	prev=$(read_state_hash || true)
	if [ -n "$prev" ] && [ "$cur" = "$prev" ]; then
		log "event=reload_skipped reason=unchanged_hash hash=${cur}"
		exit 0
	fi

	if ! run_validate "$cur"; then
		restore_last_good || true
		exit 1
	fi

	post=$(file_sha256) || {
		log "event=config_unreadable path=${CFG}"
		exit 1
	}
	if [ "$post" != "$cur" ]; then
		log "event=debounce_window_reset reason=mutated_during_validate hash=${post}"
		continue
	fi

	snapshot_last_good

	# LAST_GOOD and STATE are written only after a confirmed reload (below).
	# SIGKILL here (TimeoutStartSec) leaves last-good + state on the previous
	# commit; the live file may be new. The next trigger retries.

	if ! run_reload; then
		restore_last_good || true
		exit 1
	fi

	post=$(file_sha256) || {
		log "event=config_unreadable path=${CFG}"
		exit 1
	}
	if [ "$post" != "$cur" ]; then
		log "event=debounce_window_reset reason=mutated_during_reload hash=${post}"
		continue
	fi

	cp -a -- "$CFG" "$LAST_GOOD"
	write_state "$cur"
	log "event=last_good_updated hash=${cur}"
	exit 0
done
