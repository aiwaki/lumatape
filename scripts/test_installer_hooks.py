"""Compile the real NSIS hook; on Windows execute it against isolated registry hives.

The fixture does not override product macros or touch Edge's registry entries.
RegOverridePredefKey redirects only the fixture process's HKLM/HKCU handles into
one disposable user key. Its embedded bootstrapper writes to that explicit key.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import uuid


ROOT = Path(__file__).resolve().parent.parent
HOOK = ROOT / 'desktop/src-tauri/installer-hooks.nsh'
GUID = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
USER_KEY = rf'SOFTWARE\Microsoft\EdgeUpdate\Clients\{GUID}'
MACHINE_KEY = rf'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{GUID}'


def nsis_path(path):
    # Generated paths have no user-controlled NSIS substitutions.
    return str(path).replace('$', '$$').replace('"', '$\\"')


def compiler():
    found = shutil.which('makensis')
    if found:
        return found
    if os.name == 'nt':
        path = Path(os.environ.get('ProgramFiles(x86)', 'C:/Program Files (x86)')) / 'NSIS/makensis.exe'
        if path.is_file():
            return str(path)
    return None


BOOTSTRAPPER = r'''
Unicode true
Name "LumaTape isolated bootstrapper fixture"
OutFile "@OUTPUT@"
RequestExecutionLevel user
SilentInstall silent
Section
  SetRegView 64
  ReadEnvStr $0 "LUMATAPE_WEBVIEW_TEST_KEY"
  ReadEnvStr $1 "LUMATAPE_WEBVIEW_TEST_MODE"
  ReadEnvStr $2 "LUMATAPE_WEBVIEW_TEST_DIRECTORY"
  StrCmp $0 "" fixture_invalid
  StrCmp $2 "" fixture_invalid
  FileOpen $3 "$2\bootstrap.called" w
  FileWrite $3 "called"
  FileClose $3
  StrCmp $1 "success-user" fixture_user
  StrCmp $1 "nonzero-with-pv" fixture_user
  StrCmp $1 "success-machine" fixture_machine
  StrCmp $1 "zero-pv" fixture_zero
  Goto fixture_exit
  fixture_user:
    WriteRegStr HKCU "$0\user\@USER_KEY@" "pv" "140.0.1.2"
    Goto fixture_exit
  fixture_machine:
    WriteRegStr HKCU "$0\machine\@MACHINE_KEY@" "pv" "140.0.1.2"
    Goto fixture_exit
  fixture_zero:
    WriteRegStr HKCU "$0\user\@USER_KEY@" "pv" "0.0.0.0"
  fixture_exit:
    StrCmp $1 "nonzero" fixture_invalid
    StrCmp $1 "nonzero-with-pv" fixture_invalid
    SetErrorLevel 0
    Quit
  fixture_invalid:
    SetErrorLevel 5
    Quit
SectionEnd
'''

HARNESS = r'''
Unicode true
!include LogicLib.nsh
!macro CheckIfAppIsRunning executableName productName
  !error "STOCK_KILL_MACRO_MUST_NOT_EXPAND"
!macroend
!include "@HOOK@"
!define ARCH "x64"
!define INSTALLWEBVIEW2MODE "embedBootstrapper"
!define WEBVIEW2APPGUID "@GUID@"
!define WEBVIEW2INSTALLERARGS "/silent"
!define WEBVIEW2BOOTSTRAPPERPATH "@BOOTSTRAPPER@"
Var UpdateMode
Var MachineHandle
Var UserHandle
Name "LumaTape isolated installer hook fixture"
OutFile "@OUTPUT@"
RequestExecutionLevel user
SilentInstall silent
LangString installingWebview2 1033 "Installing test runtime"
LangString webview2InstallSuccess 1033 "Test runtime installed"
Section
  SetRegView 64
  WriteRegStr HKCU "@REGISTRY@\machine" "fixture" "owned"
  WriteRegStr HKCU "@REGISTRY@\user" "fixture" "owned"
  @MACHINE_INITIAL@
  @USER_INITIAL@
  ; Match SetRegView 64 when opening handles from this 32-bit NSIS process:
  ; KEY_ALL_ACCESS | KEY_WOW64_64KEY.
  System::Call 'advapi32::RegOpenKeyExW(p 0x80000001, w "@REGISTRY@\machine", i 0, i 0xF013F, *p.r0) i.r1'
  StrCmp $1 0 0 fixture_setup_failed
  StrCpy $MachineHandle $0
  System::Call 'advapi32::RegOpenKeyExW(p 0x80000001, w "@REGISTRY@\user", i 0, i 0xF013F, *p.r0) i.r1'
  StrCmp $1 0 0 fixture_setup_failed
  StrCpy $UserHandle $0
  System::Call 'advapi32::RegOverridePredefKey(p 0x80000002, p $MachineHandle) i.r1'
  StrCmp $1 0 0 fixture_setup_failed
  System::Call 'advapi32::RegOverridePredefKey(p 0x80000001, p $UserHandle) i.r1'
  StrCmp $1 0 0 fixture_setup_failed
  StrCpy $UpdateMode @UPDATE@
  StrCpy $R0 "preserve-r0"
  StrCpy $R1 "preserve-r1"
  !insertmacro NSIS_HOOK_PREINSTALL
  StrCmp $R0 "preserve-r0" 0 fixture_setup_failed
  StrCmp $R1 "preserve-r1" 0 fixture_setup_failed
  StrCmp $UpdateMode @UPDATE@ 0 fixture_setup_failed
  ; Stand-in for Tauri's first application File directive after PREINSTALL.
  FileOpen $0 "$EXEDIR\original.txt" w
  FileWrite $0 "new"
  FileClose $0
  SetErrorLevel 0
  Quit
  fixture_setup_failed:
    SetErrorLevel 99
    Quit
SectionEnd
'''


class InstallerHookTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.makensis = compiler()
        if not cls.makensis:
            if os.name == 'nt':
                raise RuntimeError('NSIS compiler required for Windows installer hook tests; install NSIS')
            raise unittest.SkipTest('NSIS compiler unavailable; install NSIS to verify installer hooks')
        print('Installer hook verification: ' + (
            'compilation and isolated Windows runtime scenarios' if os.name == 'nt'
            else 'compilation only; registry/process scenarios require Windows'), flush=True)
        cls.temporary = tempfile.TemporaryDirectory(prefix='lumatape-hook-')
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.area = Path(cls.temporary.name) / 'Luma Tape тест'
        cls.area.mkdir()
        cls.bootstrapper = cls.area / 'fixture bootstrapper.exe'
        cls.compile('bootstrapper', BOOTSTRAPPER.replace('@OUTPUT@', nsis_path(cls.bootstrapper))
                    .replace('@USER_KEY@', USER_KEY).replace('@MACHINE_KEY@', MACHINE_KEY))
        cls.bad_bootstrapper = cls.area / 'not a program.exe'
        cls.bad_bootstrapper.write_bytes(b'This is deliberately not a Windows executable.\n')

    @classmethod
    def compile(cls, name, source):
        path = cls.area / (name + '.nsi')
        path.write_text(source, encoding='utf-8-sig')
        verbosity = '/V2' if os.name == 'nt' else '-V2'
        result = subprocess.run([cls.makensis, verbosity, str(path)], cwd=cls.area,
                                capture_output=True, text=True, errors='replace', timeout=30)
        if result.returncode:
            raise AssertionError('NSIS fixture compilation failed:\n' + result.stdout + result.stderr)

    def fixture(self, name, *, machine='', user='', mode='success-user', update=1, succeeds=True, calls=1):
        registry = 'Software\\LumaTapeInstallerTests\\' + uuid.uuid4().hex
        output = self.area / (name + '.exe')
        source = HARNESS
        values = dict(HOOK=nsis_path(HOOK), GUID=GUID, BOOTSTRAPPER=nsis_path(
            self.bad_bootstrapper if mode == 'launch-failure' else self.bootstrapper),
            OUTPUT=nsis_path(output), REGISTRY=registry, UPDATE=str(update),
            MACHINE_INITIAL=f'WriteRegStr HKCU "{registry}\\machine\\{MACHINE_KEY}" "pv" "{machine}"' if machine else '',
            USER_INITIAL=f'WriteRegStr HKCU "{registry}\\user\\{USER_KEY}" "pv" "{user}"' if user else '')
        for key, value in values.items():
            source = source.replace('@' + key + '@', value)
        self.compile(name, source)
        if os.name != 'nt':
            return  # Cross-host test proves compilation only; Windows runs the fixture.
        marker = self.area / 'original.txt'
        marker.write_text('old', encoding='ascii')
        called = self.area / 'bootstrap.called'
        called.unlink(missing_ok=True)
        temporary = self.area / 'temporary path with spaces'
        temporary.mkdir(exist_ok=True)
        env = dict(os.environ, LUMATAPE_WEBVIEW_TEST_KEY=registry,
                   LUMATAPE_WEBVIEW_TEST_MODE=mode, LUMATAPE_WEBVIEW_TEST_DIRECTORY=str(self.area),
                   TEMP=str(temporary), TMP=str(temporary))
        try:
            result = subprocess.run([str(output), '/S'], env=env, timeout=30)
            self.assertEqual(result.returncode, 0 if succeeds else 3, name)
            self.assertEqual(marker.read_text(), 'new' if succeeds else 'old', name)
            self.assertEqual(int(called.exists()), calls, name)
        finally:
            self.remove_registry_fixture(registry)

    @staticmethod
    def remove_registry_fixture(path):
        import winreg
        def remove(parent, name):
            try:
                with winreg.OpenKey(parent, name, 0, winreg.KEY_ALL_ACCESS | winreg.KEY_WOW64_64KEY) as key:
                    while True:
                        try:
                            child = winreg.EnumKey(key, 0)
                        except OSError:
                            break
                        remove(key, child)
                winreg.DeleteKeyEx(parent, name, winreg.KEY_WOW64_64KEY)
            except FileNotFoundError:
                pass
        if not path.startswith('Software\\LumaTapeInstallerTests\\'):
            raise ValueError('Refuse cleanup outside the test-owned registry namespace')
        remove(winreg.HKEY_CURRENT_USER, path)

    def test_runtime_already_present_in_either_hive(self):
        for name, machine, user in [('machine', '140.0.1.2', ''), ('user', '', '140.0.1.2'),
                                    ('zero-machine-user', '0.0.0.0', '140.0.1.2')]:
            with self.subTest(name=name):
                self.fixture(name, machine=machine, user=user, calls=0)

    def test_missing_or_zero_runtime_installs_and_checks_both_hives(self):
        for name, mode, machine, user in [('install-user', 'success-user', '', ''),
                                         ('install-machine', 'success-machine', '', ''),
                                         ('replace-zero', 'success-user', '0.0.0.0', '0.0.0.0')]:
            with self.subTest(name=name):
                self.fixture(name, mode=mode, machine=machine, user=user)

    def test_failed_bootstrapper_preserves_existing_application_files(self):
        for mode in ['launch-failure', 'nonzero', 'nonzero-with-pv', 'no-pv', 'zero-pv']:
            with self.subTest(mode=mode):
                self.fixture(mode, mode=mode, succeeds=False, calls=int(mode != 'launch-failure'))

    def test_manual_install_remains_owned_by_the_stock_tauri_section(self):
        self.fixture('manual', update=0, calls=0)


if __name__ == '__main__':
    unittest.main()
