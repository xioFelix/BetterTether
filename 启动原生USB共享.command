#!/bin/bash
cd "$(dirname "$0")" || exit 1
if [ ! -f build/ncm-helper/native-tether.jar ]; then
  bash scripts/android/build-native-tether.sh || {
    echo '辅助程序构建失败，请查看上面的错误。'
    read -r -p '按回车关闭窗口…' _
    exit 1
  }
fi
/usr/bin/python3 scripts/native-ncm.py start
result=$?
if [ "$result" -ne 0 ]; then
  echo '未能确认连接成功，请查看上面的提示及 docs/NATIVE-NCM.md。'
fi
read -r -p '按回车关闭窗口…' _
exit "$result"
