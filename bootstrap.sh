#!/bin/sh

set -eu

usage() {
	printf 'usage: %s [--preview-apply]\n' "$0" >&2
}

preview_apply=false
case $# in
	0) ;;
	1)
		if [ "$1" != "--preview-apply" ]; then
			usage
			exit 2
		fi
		preview_apply=true
		;;
	*)
		usage
		exit 2
		;;
esac

case ${HOME-} in
	/*) ;;
	*)
		printf 'HOME must be a non-empty absolute path\n' >&2
		exit 1
		;;
esac

repository=$(CDPATH= cd -L "$(dirname "$0")" && pwd -L)
if [ "${INSTALL_DIR+x}" = x ]; then
	install_dir=$INSTALL_DIR
else
	install_dir=$HOME/.local/bin
fi
case $install_dir in
	/*) ;;
	*)
		printf 'INSTALL_DIR must be a non-empty absolute path: %s\n' "$install_dir" >&2
		exit 1
		;;
esac

make -C "$repository" install INSTALL_DIR="$install_dir"
dot_binary=$install_dir/dot

path_has_install_dir=false
saved_ifs=$IFS
IFS=:
set -f
for path_entry in ${PATH-}; do
	if [ "$path_entry" = "$install_dir" ]; then
		path_has_install_dir=true
		break
	fi
done
set +f
IFS=$saved_ifs
if [ "$path_has_install_dir" = false ]; then
	printf 'warning: %s is not on PATH; using %s directly\n' \
		"$install_dir" "$dot_binary" >&2
fi

"$dot_binary" init "$repository"
if [ "$preview_apply" = true ]; then
	"$dot_binary" apply --dry-run
else
	"$dot_binary" apply
fi
