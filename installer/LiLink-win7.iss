; 励联 LiLink Win7 专用安装包（Inno Setup 6，Go 1.20 编译的 walk 版 GUI）
; 与主包（LiLink.iss）同 AppId：用户从 Win10 包换装 Win7 包不会装出两份。
; 内嵌 amd64+386 双架构产物，安装时按系统位数自动选择，32/64 位 Win7 通用。
#define MyAppName "励联LiLink"
#define MyAppVersion "1.1.9"
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
OutputBaseFilename=LiLink-Setup-Win7-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
DisableWelcomePage=no
PrivilegesRequired=admin
; 不设 ArchitecturesInstallIn64BitMode：保持 32 位兼容安装目录行为，与 x86 zip 分发并存

[Languages]
Name: "chs"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[CustomMessages]
chs.CreateDesktopIcon=创建桌面快捷方式(&D)
chs.LaunchProgram=立即运行 %1(&R)
chs.AdditionalTasks=附加任务：

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalTasks}"

[Files]
; 双架构通用安装包：安装时按系统位数自动选择对应 exe（32 位 Win7 装 386，64 位装 amd64）
Source: "..\dist\win7\LiYouP2P-amd64.exe"; DestName: "LiYouP2P.exe"; DestDir: "{app}"; Check: IsWin64; Flags: ignoreversion
Source: "..\dist\win7\LiYouP2P-386.exe"; DestName: "LiYouP2P.exe"; DestDir: "{app}"; Check: not IsWin64; Flags: ignoreversion
Source: "..\dist\win7\liyou-agent-amd64.exe"; DestName: "liyou-agent.exe"; DestDir: "{app}"; Check: IsWin64; Flags: ignoreversion
Source: "..\dist\win7\liyou-agent-386.exe"; DestName: "liyou-agent.exe"; DestDir: "{app}"; Check: not IsWin64; Flags: ignoreversion
Source: "..\gui-wails\build\windows\icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\LiYouP2P.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\LiYouP2P.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\LiYouP2P.exe"; Description: "{cm:LaunchProgram,{#MyAppName}}"; Flags: nowait postinstall skipifsilent

[Code]
// 安装完成（提权上下文）预置防火墙入站 UDP 放行规则：跨网打洞必需，免去运行期 UAC。
const
  FWRuleName = 'LiYou P2P Agent UDP-In';

var
  RC: Integer;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    if not Exec(ExpandConstant('{sys}\netsh.exe'),
      'advfirewall firewall show rule name="' + FWRuleName + '"', '', SW_HIDE, ewWaitUntilTerminated, RC) then
      Exec(ExpandConstant('{sys}\netsh.exe'),
        'advfirewall firewall add rule name="' + FWRuleName + '" dir=in action=allow protocol=UDP program="' +
        ExpandConstant('{app}\liyou-agent.exe') + '"', '', SW_HIDE, ewWaitUntilTerminated, RC);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    Exec(ExpandConstant('{sys}\netsh.exe'),
      'advfirewall firewall delete rule name="' + FWRuleName + '"', '', SW_HIDE, ewWaitUntilTerminated, RC);
end;
