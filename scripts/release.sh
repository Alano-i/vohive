#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-${PUBLISH_VERSION:-}}"
WAIT_RELEASE="${WAIT_RELEASE:-1}"

die() {
	echo "error: $*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

latest_semver_tag() {
	git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1
}

next_patch_version() {
	latest="$(latest_semver_tag)"
	if [ -z "$latest" ]; then
		echo "v0.1.0"
		return
	fi
	major="$(printf '%s' "$latest" | sed -n 's/^v\([0-9][0-9]*\)\.\([0-9][0-9]*\)\.\([0-9][0-9]*\)$/\1/p')"
	minor="$(printf '%s' "$latest" | sed -n 's/^v\([0-9][0-9]*\)\.\([0-9][0-9]*\)\.\([0-9][0-9]*\)$/\2/p')"
	patch="$(printf '%s' "$latest" | sed -n 's/^v\([0-9][0-9]*\)\.\([0-9][0-9]*\)\.\([0-9][0-9]*\)$/\3/p')"
	[ -n "$major" ] && [ -n "$minor" ] && [ -n "$patch" ] || die "cannot derive next version from $latest"
	echo "v${major}.${minor}.$((patch + 1))"
}

validate_version() {
	case "$1" in
		v[0-9]*.[0-9]*.[0-9]*)
			return 0
			;;
		*)
			die "release version must look like vX.Y.Z, got: $1"
			;;
	esac
}

ensure_clean_tree() {
	if [ -n "$(git status --porcelain)" ]; then
		git status --short
		die "working tree is not clean; commit or stash changes before release"
	fi
}

run_verify() {
	if [ "${SKIP_VERIFY:-0}" = "1" ]; then
		echo "SKIP_VERIFY=1, skip release verification"
		return
	fi
	make check-local-modules
	GOPROXY=off GOSUMDB=off GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" \
		-o /tmp/vohive-release-verify ./cmd/vohive
}

write_release_notes() {
	version="$1"
	out="$2"
	prev_tag="$(latest_semver_tag)"
	repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
	date_utc="$(date -u +'%Y-%m-%d %H:%M:%SZ')"

	if [ -n "$prev_tag" ]; then
		range="${prev_tag}..HEAD"
		changes="$(git log "$range" --pretty=format:'- %s (%h)' --no-merges || true)"
		compare_url="https://github.com/${repo}/compare/${prev_tag}...${version}"
	else
		changes="$(git log --max-count=20 --pretty=format:'- %s (%h)' --no-merges || true)"
		compare_url=""
	fi
	if [ -z "$changes" ]; then
		changes="- No code changes since ${prev_tag:-repository start}."
	fi

	{
		echo "## VoHive ${version}"
		echo
		echo "Published at: ${date_utc}"
		echo
		echo "## Changes"
		echo
		printf '%s\n' "$changes"
		echo
		echo "## Install or upgrade"
		echo
		echo '```sh'
		echo "curl -fsSL https://raw.githubusercontent.com/${repo}/main/scripts/install.sh | sudo sh -s -- --version ${version}"
		echo '```'
		echo
		echo "The installer automatically selects the matching Linux package for amd64, arm64, or armv7."
		echo
		echo "## Release assets"
		echo
		echo "- vohive_${version}_linux_amd64"
		echo "- vohive_${version}_linux_arm64"
		echo "- vohive_${version}_linux_armv7"
		echo "- install.sh"
		echo "- install-local.sh"
		echo "- uninstall.sh"
		if [ -n "$compare_url" ]; then
			echo
			echo "## Full changelog"
			echo
			echo "$compare_url"
		fi
	} > "$out"
}

wait_for_binary_workflow() {
	[ "$WAIT_RELEASE" = "1" ] || return 0
	sha="$(git rev-parse HEAD)"
	run_id=""
	i=0
	while [ "$i" -lt 60 ]; do
		run_id="$(gh run list \
			--workflow "Build Release Binaries" \
			--limit 30 \
			--json databaseId,headSha,event \
			--jq ".[] | select(.headSha == \"${sha}\" and .event == \"push\") | .databaseId" \
			| head -n 1 || true)"
		if [ -n "$run_id" ]; then
			break
		fi
		i=$((i + 1))
		sleep 5
	done
	[ -n "$run_id" ] || die "binary release workflow run was not found"
	gh run watch "$run_id" --exit-status
}

main() {
	need_cmd git
	need_cmd gh
	need_cmd go
	need_cmd make

	git fetch origin --tags
	ensure_clean_tree

	if [ -z "$VERSION" ]; then
		VERSION="$(next_patch_version)"
	fi
	validate_version "$VERSION"

	if git rev-parse -q --verify "refs/tags/${VERSION}" >/dev/null; then
		die "local tag already exists: ${VERSION}"
	fi
	if git ls-remote --exit-code --tags origin "refs/tags/${VERSION}" >/dev/null 2>&1; then
		die "remote tag already exists: ${VERSION}"
	fi
	if gh release view "$VERSION" >/dev/null 2>&1; then
		die "GitHub release already exists: ${VERSION}"
	fi

	run_verify
	notes_file="$(mktemp)"
	trap 'rm -f "$notes_file"' EXIT INT TERM
	write_release_notes "$VERSION" "$notes_file"

	branch="$(git branch --show-current)"
	[ -n "$branch" ] || die "not on a branch"

	git push origin "$branch"
	git tag -a "$VERSION" -m "Release ${VERSION}"
	git push origin "$VERSION"

	gh release create "$VERSION" --title "VoHive ${VERSION}" --notes-file "$notes_file" --latest
	wait_for_binary_workflow

	echo "Release published: https://github.com/$(gh repo view --json nameWithOwner --jq .nameWithOwner)/releases/tag/${VERSION}"
}

main "$@"
