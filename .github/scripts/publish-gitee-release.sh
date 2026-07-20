#!/usr/bin/env bash
set -euo pipefail

: "${GITEE_TOKEN:?GITEE_TOKEN is required}"
: "${VERSION:?VERSION is required}"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Refusing to publish non-release VERSION: $VERSION" >&2
  exit 1
fi

OWNER="shmilyfuu"
REPO="BiliQueue"
TAG="v${VERSION}"
API="https://gitee.com/api/v5/repos/${OWNER}/${REPO}"
TITLE="BiliQueue ${TAG}"
NOTES_FILE="${NOTES_FILE:-release-notes-current.md}"
WINDOWS_ASSET="BiliQueue-v${VERSION}-windows.zip"
WINDOWS_CHECKSUM="${WINDOWS_ASSET}.sha256"
SOURCE_ASSET="BiliQueue-v${VERSION}-source.zip"

for file in "$NOTES_FILE" "$WINDOWS_ASSET" "$WINDOWS_CHECKSUM" "$SOURCE_ASSET"; do
  if [[ ! -f "$file" ]]; then
    echo "Missing release file: $file" >&2
    exit 1
  fi
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

release_json="$tmp_dir/release.json"
status="$(curl --silent --show-error --output "$release_json" --write-out '%{http_code}' \
  --get "${API}/releases/tags/${TAG}" \
  --data-urlencode "access_token=${GITEE_TOKEN}")"

release_body="$(cat "$NOTES_FILE")"
if [[ "$status" == "200" ]]; then
  release_id="$(jq -er '.id' "$release_json")"
  curl --fail --silent --show-error --output "$release_json" \
    --request PATCH "${API}/releases/${release_id}" \
    --form-string "access_token=${GITEE_TOKEN}" \
    --form-string "tag_name=${TAG}" \
    --form-string "name=${TITLE}" \
    --form-string "body=${release_body}" \
    --form-string "prerelease=false"
elif [[ "$status" == "404" ]]; then
  curl --fail --silent --show-error --output "$release_json" \
    --request POST "${API}/releases" \
    --form-string "access_token=${GITEE_TOKEN}" \
    --form-string "tag_name=${TAG}" \
    --form-string "target_commitish=main" \
    --form-string "name=${TITLE}" \
    --form-string "body=${release_body}" \
    --form-string "prerelease=false"
  release_id="$(jq -er '.id' "$release_json")"
else
  echo "Unable to query Gitee release ${TAG} (HTTP ${status})." >&2
  cat "$release_json" >&2
  exit 1
fi

assets_json="$tmp_dir/assets.json"
curl --fail --silent --show-error --output "$assets_json" \
  --get "${API}/releases/${release_id}/attach_files" \
  --data-urlencode "access_token=${GITEE_TOKEN}"

for file in "$WINDOWS_ASSET" "$WINDOWS_CHECKSUM" "$SOURCE_ASSET"; do
  while IFS= read -r asset_id; do
    curl --fail --silent --show-error --output /dev/null \
      --request DELETE "${API}/releases/${release_id}/attach_files/${asset_id}" \
      --form-string "access_token=${GITEE_TOKEN}"
  done < <(jq -r --arg name "$(basename "$file")" '.[] | select(.name == $name) | .id' "$assets_json")

  curl --fail --silent --show-error --output /dev/null \
    --request POST "${API}/releases/${release_id}/attach_files" \
    --form-string "access_token=${GITEE_TOKEN}" \
    --form "file=@${file}"
done

releases_json="$tmp_dir/releases.json"
curl --fail --silent --show-error --output "$releases_json" \
  --get "${API}/releases" \
  --data-urlencode "access_token=${GITEE_TOKEN}" \
  --data-urlencode "page=1" \
  --data-urlencode "per_page=100" \
  --data-urlencode "direction=desc"

while IFS= read -r old_release_id; do
  curl --fail --silent --show-error --output /dev/null \
    --request DELETE "${API}/releases/${old_release_id}" \
    --form-string "access_token=${GITEE_TOKEN}"
done < <(jq -r 'sort_by(.created_at // .published_at // "") | reverse | .[5:][] | .id' "$releases_json")

echo "Published ${TAG} to Gitee and retained the newest five releases."
