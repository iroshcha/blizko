#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/core"
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20260908204917-8b95e45f8d3e
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20260908204917-8b95e45f8d3e
export PATH="$(go env GOPATH)/bin:$PATH"
gomobile init
mkdir -p "$ROOT/ios/Frameworks"
gomobile bind -target=ios,iossimulator -tags=ts_omit_logtail -iosversion=16.0 -ldflags='-s -w' -o "$ROOT/ios/Frameworks/Mobile.xcframework" ./mobile
cd "$ROOT/ios"
xcodegen generate
xcodebuild -project Blizko.xcodeproj -scheme Blizko -configuration Release -sdk iphoneos -destination 'generic/platform=iOS' -derivedDataPath build CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
mkdir -p "$ROOT/dist/ios/Payload"
cp -R build/Build/Products/Release-iphoneos/Blizko.app "$ROOT/dist/ios/Payload/"
cd "$ROOT/dist/ios"
zip -qry ../Blizko-unsigned.ipa Payload
