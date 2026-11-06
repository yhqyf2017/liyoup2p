; 励联 LiLink 桌面安装包（Inno Setup 6）
; 一个安装包覆盖客户端/服务端双角色：GUI 首次启动自选角色，故不做两份。
#define MyAppName "励联LiLink"
#define MyAppVersion "1.0.2"
#define MyAppPublisher "济源市励友软件有限公司"

[Setup]
AppId={{9F3C2B7A-5D1E-4A6B-8C2F-1E2D3A4B5C6D}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\LiLink
DefaultGroupName={#MyAppName}
SetupIconFile=..\gui-wails\build\windows\icon.ico
WizardImageFile=assets\LiLink-WizardImage.bmp
WizardSmallImageFile=assets\LiLink-WizardSmall.bmp
UninstallDisplayIcon={app}\icon.ico
OutputDir=dist
OutputBaseFilename=LiLink-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
DisableWelcomePage=no
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "chs"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[CustomMessages]
chs.CreateDesktopIcon=创建桌面快捷方式(&D)
chs.LaunchProgram=立即运行 %1(&R)
chs.AdditionalTasks=附加任务：

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalTasks}"

[Files]
Source: "..\LiYouP2P.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\liyou-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\gui-wails\build\windows\icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\LiYouP2P.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\LiYouP2P.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\LiYouP2P.exe"; Description: "{cm:LaunchProgram,{#MyAppName}}"; Flags: nowait postinstall skipifsilent
