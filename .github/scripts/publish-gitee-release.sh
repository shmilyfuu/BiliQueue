#!/usr/bin/env bash
set -euo pipefail
trap 'echo "Gitee release script failed at line ${LINENO}." >&2' ERR

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

echo "Validating Gitee API token..."
user_json="$tmp_dir/user.json"
status="$(curl --silent --show-error --output "$user_json" --write-out '%{http_code}' \
  --get "https://gitee.com/api/v5/user" \
  --data-urlencode "access_token=${GITEE_TOKEN}")"
if [[ "$status" != "200" ]]; then
  echo "Gitee API token validation failed (HTTP ${status})." >&2
  cat "$user_json" >&2
  exit 1
fi

echo "Querying Gitee release ${TAG}..."
release_json="$tmp_dir/release.json"
status="$(curl --silent --show-error --output "$release_json" --write-out '%{http_code}' \
  --get "${API}/releases/tags/${TAG}" \
  --data-urlencode "access_token=${GITEE_TOKEN}")"

release_payload="$tmp_dir/release-payload.json"
echo "Preparing Gitee release metadata..."
jq -n \
  --arg access_token "$GITEE_TOKEN" \
  --arg tag_name "$TAG" \
  --arg target_commitish "main" \
  --arg name "$TITLE" \
  --rawfile body "$NOTES_FILE" \
  '{access_token: $access_token, tag_name: $tag_name, target_commitish: $target_commitish, name: $name, body: $body, prerelease: false}' \
  > "$release_payload"

if [[ "$status" == "200" ]]; then
  echo "Updating existing Gitee release ${TAG}..."
  release_id="$(jq -er '.id' "$release_json")"
  status="$(curl --silent --show-error --output "$release_json" --write-out '%{http_code}' \
    --request PATCH "${API}/releases/${release_id}" \
    --header "Content-Type: application/json" \
    --data-binary "@${release_payload}")"
  expected_status="200"
elif [[ "$status" == "404" ]]; then
  echo "Creating Gitee release ${TAG}..."
  status="$(curl --silent --show-error --output "$release_json" --write-out '%{http_code}' \
    --request POST "${API}/releases" \
    --header "Content-Type: application/json" \
    --data-binary "@${release_payload}")"
  expected_status="201"
else
  echo "Unable to query Gitee release ${TAG} (HTTP ${status})." >&2
  cat "$release_json" >&2
  exit 1
fi

if [[ "$status" != "$expected_status" ]]; then
  echo "Unable to publish Gitee release ${TAG} (HTTP ${status})." >&2
  cat "$release_json" >&2
  exit 1
fi
release_id="$(jq -er '.id' "$release_json")"

assets_json="$tmp_dir/assets.json"
echo "Loading existing Gitee release attachments..."
curl --fail --silent --show-error --output "$assets_json" \
  --get "${API}/releases/${release_id}/attach_files" \
  --data-urlencode "access_token=${GITEE_TOKEN}"

for file in "$WINDOWS_ASSET" "$WINDOWS_CHECKSUM" "$SOURCE_ASSET"; do
  while IFS= read -r asset_id; do
    curl --fail --silent --show-error --output /dev/null \
      --request DELETE "${API}/releases/${release_id}/attach_files/${asset_id}" \
      --get \
      --data-urlencode "access_token=${GITEE_TOKEN}"
  done < <(jq -r --arg name "$(basename "$file")" '.[] | select(.name == $name) | .id' "$assets_json")

  echo "Uploading $(basename "$file") to Gitee..."
  curl --fail --silent --show-error --output /dev/null \
    --request POST "${API}/releases/${release_id}/attach_files" \
    --form-string "access_token=${GITEE_TOKEN}" \
    --form "file=@${file}"
done

releases_json="$tmp_dir/releases.json"
echo "Applying Gitee release retention policy..."
curl --fail --silent --show-error --output "$releases_json" \
  --get "${API}/releases" \
  --data-urlencode "access_token=${GITEE_TOKEN}" \
  --data-urlencode "page=1" \
  --data-urlencode "per_page=100" \
  --data-urlencode "direction=desc"

while IFS= read -r old_release_id; do
  curl --fail --silent --show-error --output /dev/null \
    --request DELETE "${API}/releases/${old_release_id}" \
    --get \
    --data-urlencode "access_token=${GITEE_TOKEN}"
done < <(jq -r 'sort_by(.created_at // .published_at // "") | reverse | .[5:][] | .id' "$releases_json")

echo "Published ${TAG} to Gitee and retained the newest five releases."
