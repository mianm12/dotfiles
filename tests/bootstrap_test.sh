#!/bin/sh

set -eu

fail() {
	printf 'bootstrap smoke test: %s\n' "$1" >&2
	exit 1
}

mode_of() {
	case $(uname -s) in
		Darwin) stat -f '%Lp' "$1" ;;
		Linux) stat -c '%a' "$1" ;;
		*) fail "unsupported test platform" ;;
	esac
}

identity_of() {
	case $(uname -s) in
		Darwin) stat -f '%d:%i:%m:%z' "$1" ;;
		Linux) stat -c '%d:%i:%Y:%s' "$1" ;;
		*) fail "unsupported test platform" ;;
	esac
}

source_root=$(CDPATH= cd -P "$(dirname "$0")/.." && pwd -P)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/dot-bootstrap-test.XXXXXX")
trap 'rm -rf "$test_root"' 0 1 2 3 15

repository=$test_root/repository
test_home=$test_root/home
install_root=$test_root/'install*:literal'
install_dir=$install_root/bin
build_root=$test_root/build
build_binary=$build_root/dot
config_path=$test_home/.config/dot/config.toml
state_path=$test_home/.local/state/dot/state.json
target_path=$test_home/.config/starship.toml
go_cache=${GOCACHE:-$(go env GOCACHE)}
go_module_cache=${GOMODCACHE:-$(go env GOMODCACHE)}
test_path=$test_root/installX:literal/bin:${PATH-}
mkdir -p "$repository" "$test_home"
tracked_list=$test_root/tracked-files
git -C "$source_root" ls-files --cached --others --exclude-standard -- \
	Makefile go.mod go.sum dot.toml cmd internal \
	modules/starship/module.toml modules/starship/starship.toml >"$tracked_list"
while IFS= read -r relative; do
	[ -e "$source_root/$relative" ] || continue
	mkdir -p "$repository/$(dirname "$relative")"
	cp "$source_root/$relative" "$repository/$relative"
done <"$tracked_list"
cp "$source_root/bootstrap.sh" "$repository/bootstrap.sh"
chmod 0755 "$repository/bootstrap.sh"
module_files=$(find "$repository/modules" -type f -print | wc -l | tr -d ' ')
[ "$module_files" -eq 2 ] || fail "temporary repository copied unexpected module files"

run_bootstrap() {
	(
		cd "$test_root"
		HOME="$test_home" \
			INSTALL_DIR="$install_dir" \
			BINARY="$build_binary" \
			GOCACHE="$go_cache" \
			GOMODCACHE="$go_module_cache" \
			PATH="$test_path" \
			"$repository/bootstrap.sh" "$@"
	)
}

if run_bootstrap --unknown >"$test_root/invalid.out" 2>"$test_root/invalid.err"; then
	fail "invalid argument succeeded"
else
	status=$?
fi
[ "$status" -eq 2 ] || fail "invalid argument returned $status instead of 2"
[ ! -e "$install_root" ] || fail "invalid argument created install paths"
[ ! -e "$config_path" ] || fail "invalid argument created machine config"
[ ! -e "$build_root" ] || fail "invalid argument created build paths"
[ -z "$(find "$test_home" -mindepth 1 -print -quit)" ] ||
	fail "invalid argument mutated HOME"

direct_install=$test_root/direct-install/bin
HOME="$test_home" \
	BINARY="$build_binary" \
	GOCACHE="$go_cache" \
	GOMODCACHE="$go_module_cache" \
	make -C "$repository" install INSTALL_DIR="$direct_install" \
	>"$test_root/install.out" 2>"$test_root/install.err"
[ -f "$direct_install/dot" ] || fail "make install did not create a regular binary"
[ ! -L "$direct_install/dot" ] || fail "make install created a symlink"
[ -x "$direct_install/dot" ] || fail "make install binary is not executable"
[ "$(mode_of "$direct_install")" = 755 ] || fail "make install directory mode is not 0755"
[ "$(mode_of "$direct_install/dot")" = 755 ] || fail "make install binary mode is not 0755"
[ -z "$(find "$direct_install" -name '.dot.tmp.*' -print -quit)" ] ||
	fail "make install left a temporary file"

blocked_install=$test_root/blocked-install/bin
mkdir -p "$blocked_install/dot"
printf 'preserve\n' >"$blocked_install/dot/marker"
if HOME="$test_home" \
	BINARY="$build_binary" \
	GOCACHE="$go_cache" \
	GOMODCACHE="$go_module_cache" \
	make -C "$repository" install INSTALL_DIR="$blocked_install" \
	>"$test_root/blocked-install.out" 2>"$test_root/blocked-install.err"; then
	fail "make install accepted a directory destination"
fi
[ "$(cat "$blocked_install/dot/marker")" = preserve ] ||
	fail "failed install changed the destination directory"
[ -z "$(find "$blocked_install" -name '.dot.tmp.*' -print -quit)" ] ||
	fail "failed install left a temporary file"

run_bootstrap --preview-apply >"$test_root/preview.out" 2>"$test_root/preview.err"
[ -f "$install_dir/dot" ] || fail "installed binary is not a regular file"
[ ! -L "$install_dir/dot" ] || fail "installed binary is a symlink"
[ -x "$install_dir/dot" ] || fail "installed binary is not executable"
[ "$(mode_of "$install_dir")" = 755 ] || fail "new install directory mode is not 0755"
[ "$(mode_of "$install_dir/dot")" = 755 ] || fail "installed binary mode is not 0755"
[ -z "$(find "$install_dir" -name '.dot.tmp.*' -print -quit)" ] ||
	fail "install left a temporary file"
[ -f "$config_path" ] || fail "preview did not initialize machine config"
[ ! -e "$state_path" ] || fail "preview created ownership state"
[ ! -e "$target_path" ] || fail "preview changed the managed target"
grep -F 'selection_changed=true' "$test_root/preview.out" >/dev/null ||
	fail "preview did not report first init"
grep -F 'link module=starship' "$test_root/preview.out" >/dev/null ||
	fail "preview did not report starship convergence"
grep -F 'is not on PATH' "$test_root/preview.err" >/dev/null ||
	fail "preview did not warn about PATH"

run_bootstrap >"$test_root/apply.out" 2>"$test_root/apply.err"
[ -L "$target_path" ] || fail "actual bootstrap did not create starship link"
[ "$(readlink "$target_path")" = "$repository/modules/starship/starship.toml" ] ||
	fail "starship link has the wrong destination"
[ -f "$state_path" ] || fail "actual bootstrap did not create ownership state"

target_before=$(identity_of "$target_path")
target_dest_before=$(readlink "$target_path")
state_before=$(identity_of "$state_path")
state_checksum_before=$(cksum <"$state_path")
run_bootstrap >"$test_root/repeat.out" 2>"$test_root/repeat.err"
[ "$(identity_of "$target_path")" = "$target_before" ] ||
	fail "repeated bootstrap replaced the converged target"
[ "$(readlink "$target_path")" = "$target_dest_before" ] ||
	fail "repeated bootstrap changed the target destination"
[ "$(identity_of "$state_path")" = "$state_before" ] ||
	fail "repeated bootstrap rewrote ownership state"
[ "$(cksum <"$state_path")" = "$state_checksum_before" ] ||
	fail "repeated bootstrap changed ownership state content"
grep -F 'selection_changed=false' "$test_root/repeat.out" >/dev/null ||
	fail "repeated bootstrap did not report init no-op"
grep -F 'converged' "$test_root/repeat.out" >/dev/null ||
	fail "repeated bootstrap did not report convergence no-op"
