Unicode true
!include "wails_tools.nsh"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion "${INFO_PRODUCTVERSION}.0"
VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright" "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName" "${INFO_PRODUCTNAME}"

ManifestDPIAware true
!include "MUI.nsh"
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
    !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime
    SetOutPath $INSTDIR
    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    WriteRegStr HKCU "Software\Classes\entcoin" "" "Entcoin Agent Pay link"
    WriteRegStr HKCU "Software\Classes\entcoin" "URL Protocol" ""
    WriteRegStr HKCU "Software\Classes\entcoin\DefaultIcon" "" "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\",0"
    WriteRegStr HKCU "Software\Classes\entcoin\shell\open\command" "" "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" $\"%1$\""

    !insertmacro wails.associateFiles
    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext
    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    ReadRegStr $0 HKCU "Software\Classes\entcoin\shell\open\command" ""
    StrCmp $0 "$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" $\"%1$\"" 0 protocol_not_owned
    DeleteRegKey HKCU "Software\Classes\entcoin"
protocol_not_owned:

    !insertmacro wails.unassociateFiles
    !insertmacro wails.deleteUninstaller
    RMDir /r $INSTDIR
SectionEnd
