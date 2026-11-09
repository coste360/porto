; Inno Setup 6 script of the Porto desktop edition: per-user install (no administrator rights) into
; %LOCALAPPDATA%\Programs\Porto, Start menu shortcut, uninstaller that keeps the data folder (%LOCALAPPDATA%\Porto).
; Defines passed by scripts/desktop/package-windows.sh: AppVersion, SourceDir, OutputDir, OutputName, and PortoSign
; with the sign tool "porto" (/Sporto=...) when WINDOWS_SIGN_CMD is set: Inno Setup then signs the uninstaller it
; embeds and the setup itself with the same hook as porto-desktop.exe (scripts/desktop/inno-sign.sh).

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6F0E6C1B-3B7E-4C55-9A57-2E8D5B1C9A10}
AppName=Porto
AppVersion={#AppVersion}
AppVerName=Porto {#AppVersion}
AppPublisher=Coste360 SRL
DefaultDirName={localappdata}\Programs\Porto
DefaultGroupName=Porto
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename={#OutputName}
SetupIconFile={#SourceDir}\porto.ico
UninstallDisplayIcon={app}\porto-desktop.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
#ifdef PortoSign
SignTool=porto
SignedUninstaller=yes
#endif

[Languages]
Name: "it"; MessagesFile: "compiler:Languages\Italian.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\porto-desktop.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Porto"; Filename: "{app}\porto-desktop.exe"
Name: "{autodesktop}\Porto"; Filename: "{app}\porto-desktop.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\porto-desktop.exe"; Description: "{cm:LaunchProgram,Porto}"; Flags: nowait postinstall skipifsilent

; The data folder (%LOCALAPPDATA%\Porto: database, backups, keys) is never removed by the uninstaller.
