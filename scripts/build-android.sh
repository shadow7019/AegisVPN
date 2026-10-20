#!/usr/bin/env bash
#
# build-android.sh — one-shot builder for the AegisVPN Android app.
#
#   1. Verifies the Go + Android toolchains (ANDROID_HOME, NDK, JDK).
#   2. Installs gomobile if missing and compiles the aegislib AAR from
#      engine/cmd/aegislib (docs/PROTOCOL.md, "Android gomobile binding"):
#        gomobile bind -target=android -androidapi 24 \
#            -o ../android/app/libs/aegis.aar ./cmd/aegislib
#   3. Regenerates the Gradle wrapper (gradle-wrapper.jar is not checked in).
#   4. Runs `./gradlew assembleRelease`.
#   5. Copies the APK to dist/AegisVPN.apk and prints the path.
#
# Signing: set AEGIS_KEYSTORE, AEGIS_KEYSTORE_PASSWORD, AEGIS_KEY_ALIAS and
# AEGIS_KEY_PASSWORD to sign the release build with a real key. When unset the
# build falls back to the auto-generated debug key so the APK stays installable
# (fine for development, NOT for distribution).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AEGIS_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ANDROID_DIR="${AEGIS_ROOT}/android"
ENGINE_DIR="${AEGIS_ROOT}/engine"
DIST_DIR="${AEGIS_ROOT}/dist"
AAR_PATH="${ANDROID_DIR}/app/libs/aegis.aar"
GRADLE_VERSION="8.9"

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- toolchains
command -v go >/dev/null 2>&1 || die "Go toolchain not found in PATH (https://go.dev/dl)."
command -v java >/dev/null 2>&1 || die "JDK not found in PATH (Gradle + AGP need JDK 17)."

if [ -z "${ANDROID_HOME:-}" ] && [ -z "${ANDROID_SDK_ROOT:-}" ]; then
    for candidate in "${HOME}/Android/Sdk" "/usr/local/lib/android/sdk" "/opt/android-sdk"; do
        if [ -d "${candidate}" ]; then
            export ANDROID_HOME="${candidate}"
            break
        fi
    done
else
    export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT}}"
fi
[ -n "${ANDROID_HOME:-}" ] && [ -d "${ANDROID_HOME}" ] || die "Android SDK not found; set ANDROID_HOME."
log "ANDROID_HOME=${ANDROID_HOME}"

if ! compgen -G "${ANDROID_HOME}/ndk/*" >/dev/null; then
    die "No NDK under ${ANDROID_HOME}/ndk — install one with: sdkmanager 'ndk;27.0.12077973'"
fi

# ------------------------------------------------------------- aegislib AAR
[ -d "${ENGINE_DIR}/cmd/aegislib" ] || \
    die "engine/cmd/aegislib not found — the Go gomobile binding must exist first."

log "Building gomobile binding aegislib -> android/app/libs/aegis.aar"
export PATH="${PATH}:$(go env GOPATH)/bin"
if ! command -v gomobile >/dev/null 2>&1; then
    log "Installing gomobile (golang.org/x/mobile/cmd/gomobile@latest)"
    go install golang.org/x/mobile/cmd/gomobile@latest
fi
( cd "${ENGINE_DIR}" && go mod download )
gomobile init
mkdir -p "${ANDROID_DIR}/app/libs"
( cd "${ENGINE_DIR}" && gomobile bind -target=android -androidapi 24 -o "${AAR_PATH}" ./cmd/aegislib )
[ -s "${AAR_PATH}" ] || die "gomobile bind produced no AAR at ${AAR_PATH}"

# ------------------------------------------------------------ gradle wrapper
cd "${ANDROID_DIR}"
if [ ! -f gradle/wrapper/gradle-wrapper.jar ]; then
    log "gradle-wrapper.jar missing — regenerating wrapper (Gradle ${GRADLE_VERSION})"
    GRADLE_BIN="$(command -v gradle || true)"
    if [ -z "${GRADLE_BIN}" ]; then
        CACHE="${HOME}/.aegisvpn/gradle-dist"
        ZIP="${CACHE}/gradle-${GRADLE_VERSION}-bin.zip"
        if [ ! -x "${CACHE}/gradle-${GRADLE_VERSION}/bin/gradle" ]; then
            mkdir -p "${CACHE}"
            log "Downloading Gradle ${GRADLE_VERSION} distribution"
            curl -fL --retry 3 -o "${ZIP}" \
                "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip"
            (
                cd "${CACHE}" && if command -v unzip >/dev/null 2>&1; then
                    unzip -qo "${ZIP}"
                else
                    python3 -c "import zipfile; zipfile.ZipFile('${ZIP}').extractall('${CACHE}')"
                fi
            )
        fi
        GRADLE_BIN="${CACHE}/gradle-${GRADLE_VERSION}/bin/gradle"
    fi
    "${GRADLE_BIN}" wrapper --gradle-version "${GRADLE_VERSION}"
fi

# --------------------------------------------------------------- APK build
if [ ! -f local.properties ]; then
    printf 'sdk.dir=%s\n' "${ANDROID_HOME}" > local.properties
fi

if [ -n "${AEGIS_KEYSTORE:-}" ]; then
    log "Release signing: ${AEGIS_KEYSTORE}"
else
    log "AEGIS_KEYSTORE unset — release APK will be signed with the DEBUG key (dev only)."
fi

./gradlew --no-daemon assembleRelease

APK="${ANDROID_DIR}/app/build/outputs/apk/release/app-release.apk"
if [ ! -f "${APK}" ]; then
    APK="${ANDROID_DIR}/app/build/outputs/apk/release/app-release-unsigned.apk"
fi
[ -f "${APK}" ] || die "APK not found after build."

mkdir -p "${DIST_DIR}"
cp -f "${APK}" "${DIST_DIR}/AegisVPN.apk"

log "Built: ${DIST_DIR}/AegisVPN.apk"
if [ -n "${AEGIS_KEYSTORE:-}" ]; then
    log "Signed with ${AEGIS_KEYSTORE} (alias: ${AEGIS_KEY_ALIAS:-<unset>})."
else
    log "NOTE: signed with the debug key. For distribution set AEGIS_KEYSTORE, "
    log "AEGIS_KEYSTORE_PASSWORD, AEGIS_KEY_ALIAS, AEGIS_KEY_PASSWORD and rebuild."
fi
log "Install with: adb install -r ${DIST_DIR}/AegisVPN.apk"
