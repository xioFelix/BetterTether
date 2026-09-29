#!/bin/bash
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
output="$repo/build/ncm-helper"
java_bin=${JAVA_BIN_DIR:-/opt/homebrew/opt/openjdk/bin}
mkdir -p "$output/classes" "$output/dex"
version=9.4.26
checksum=870354f719912241712d6773bfd7c66b1138b790a1ee547dc4726c6ddcf2c332
if [ ! -f "$output/r8.jar" ]; then
  curl --fail --location --max-time 120 \
    "https://dl.google.com/dl/android/maven2/com/android/tools/r8/$version/r8-$version.jar" \
    -o "$output/r8.jar.download"
  echo "$checksum  $output/r8.jar.download" | shasum -a 256 -c -
  mv "$output/r8.jar.download" "$output/r8.jar"
fi
echo "$checksum  $output/r8.jar" | shasum -a 256 -c -
"$java_bin/javac" --release 8 -Xlint:-options -d "$output/classes" "$repo/scripts/android/NativeTether.java"
"$java_bin/java" -cp "$output/r8.jar" com.android.tools.r8.D8 --min-api 26 \
  --output "$output/dex" "$output/classes/NativeTether.class"
"$java_bin/jar" --create --file "$output/native-tether.jar" -C "$output/dex" classes.dex
echo "Built $output/native-tether.jar"
