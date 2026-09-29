#!/usr/bin/env python3
"""Experimental opt-in NCM activation, tested on OnePlus PLZ110 / Android 16.

Requires an authorized USB ADB device and the locally built NativeTether helper.
Does not edit Surge, service order, Wi-Fi, or Android persistent properties.
"""
import argparse
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
HELPER = REPO / 'build/ncm-helper/native-tether.jar'
LABEL = 'system/com.s4wbvnny.bettertether'
ADB = shutil.which('adb') or '/opt/homebrew/bin/adb'


def run(args, check=True, timeout=30):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(result.stdout.strip() or 'Command failed: ' + args[0])
    return result


def adb(*args, check=True):
    # -d refuses to pick an arbitrary device if several USB phones are attached.
    return run([ADB, '-d', *args], check=check)


def wait_phone():
    for _ in range(20):
        result = adb('get-state', check=False)
        if result.returncode == 0 and result.stdout.strip() == 'device':
            return
        time.sleep(1)
    raise RuntimeError('ADB cannot open the phone. Unlock it, choose USB File Transfer instead of USB Tethering, keep USB debugging enabled, then reconnect.')


def use_native_adb_backend():
    # libusb can retain an unusable handle after the old RNDIS daemon exits.
    # The native macOS ADB backend was verified with this phone.
    os.environ['ADB_LIBUSB'] = '0'
    run([ADB, 'kill-server'], check=False)
    run([ADB, 'start-server'])


def ncm_interfaces():
    output = run(['/usr/sbin/ioreg', '-r', '-c', 'AppleUSBNCMControl', '-l', '-w0']).stdout
    return sorted(set(re.findall(r'"BSD Name" = "(en\d+)"', output)))


def verify_dhcp():
    interfaces = ncm_interfaces()
    if len(interfaces) != 1:
        return None
    interface = interfaces[0]
    packet = run(['/usr/sbin/ipconfig', 'getpacket', interface], check=False).stdout
    if 'ACK' not in packet or 'router (ip_mult)' not in packet:
        return None
    address = run(['/usr/sbin/ipconfig', 'getifaddr', interface], check=False).stdout.strip()
    if not address or address.startswith('169.254.'):
        return None
    return interface, address


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['start', 'status'])
    action = parser.parse_args().action
    if action == 'status':
        state = verify_dhcp()
        print('NCM DHCP ready: %s, %s' % state if state else 'No single NCM interface with a DHCP gateway.')
        print(run(['/sbin/route', '-n', 'get', 'default'], check=False).stdout)
        return
    if not HELPER.is_file():
        raise RuntimeError('Build the helper first: bash scripts/android/build-native-tether.sh')
    old_daemon = run(['/bin/launchctl', 'print', LABEL], check=False).returncode == 0
    if old_daemon:
        print('Stopping BetterTether RNDIS relay to release USB. macOS may request an administrator password.', flush=True)
        subprocess.run(['sudo', '/bin/launchctl', 'bootout', LABEL], check=True)
    try:
        use_native_adb_backend()
        wait_phone()
        # Fail before changing USB mode if this phone already uses a physical
        # Ethernet adapter; start-ethernet selects Android's tethering interface.
        ethernet = adb('shell', 'dumpsys ethernet').stdout
        match = re.search(r'Interface used for tethering: (\S+)', ethernet)
        if match and match.group(1) not in ('null', 'None') and not re.fullmatch(r'usb\d+', match.group(1)):
            raise RuntimeError('Android selected another Ethernet adapter; disconnect it before using this helper.')
        adb('push', str(HELPER), '/data/local/tmp/bettertether-native.jar')
        adb('shell', 'chmod 444 /data/local/tmp/bettertether-native.jar')
        functions = adb('shell', 'svc usb getFunctions').stdout.strip()
        if functions != 'ncm':
            # USB re-enumeration can terminate ADB even when setFunctions succeeds.
            adb('shell', 'svc usb setFunctions ncm', check=False)
            wait_phone()
        if adb('shell', 'svc usb getFunctions').stdout.strip() != 'ncm':
            raise RuntimeError('Phone did not enter NCM mode.')
        for _ in range(10):
            ethernet = adb('shell', 'dumpsys ethernet').stdout
            if re.search(r'Interface used for tethering: usb\d+\b', ethernet):
                break
            time.sleep(1)
        else:
            raise RuntimeError('No Android USB Ethernet interface available for sharing.')
        # USB can reset while app_process reports the callback, making ADB
        # return 255 despite a successful request. DHCP is the success test.
        result = adb('shell', 'CLASSPATH=/data/local/tmp/bettertether-native.jar app_process / NativeTether start-ethernet', check=False)
        print(result.stdout.strip())
        for _ in range(25):
            state = verify_dhcp()
            if state:
                print('NCM DHCP ready: %s, %s' % state)
                probe = run(['/usr/bin/curl', '--noproxy', '*', '-4', '--interface', state[0],
                             '--fail', '--silent', '--show-error', '--max-time', '12',
                             '--output', '/dev/null', '--write-out', '%{http_code}',
                             'https://www.apple.com/'], check=False, timeout=15)
                if probe.returncode or probe.stdout.strip() != '200':
                    raise RuntimeError('DHCP succeeded but the USB Internet probe failed: '
                                       + probe.stdout.strip())
                print('Native USB Internet probe: HTTP 200')
                print('Check Network service order and Internet access. The BetterTether RNDIS relay stays stopped.')
                return
            time.sleep(1)
        raise RuntimeError('NCM interface did not obtain a DHCP gateway.')
    except Exception:
        # Preserve a successful native connection on a diagnostic error. If the
        # old RNDIS relay was stopped, provide explicit recovery rather than
        # silently claiming either mode works.
        print('Activation not confirmed. Recovery steps: docs/NATIVE-NCM.md', file=sys.stderr)
        if old_daemon:
            print('The previous BetterTether relay is stopped; recover it after returning USB to RNDIS.', file=sys.stderr)
        raise


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, subprocess.SubprocessError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
