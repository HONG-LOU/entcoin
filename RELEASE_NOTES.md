# Entcoin v1.2.2

English | [简体中文](#简体中文)

Entcoin v1.2.2 is a Windows updater reliability release for
`entropy-mainnet-v1`. It does not change consensus, genesis, the activation at
block `125555`, wallets, keys, addresses, balances, transaction encoding,
SQLite schema, chain data, peer compatibility, or the existing data directory.

## Windows update fix

The previous Windows updater always restarted
`%LOCALAPPDATA%\Programs\Entcoin\Entcoin.exe` after running the installer. That
path could differ from the EXE that initiated the update, especially for a
portable copy, a renamed executable, a custom location, or a machine containing
multiple Entcoin copies. The update could therefore appear to finish and then
open an older binary.

Starting with v1.2.2, Windows updates download the signed-or-checksummed portable
`Entcoin.exe` artifact and launch that new binary in a restricted helper mode.
The helper:

- waits for the exact old process ID to exit;
- stages the verified bytes beside the currently running EXE;
- hashes the staged copy and keeps a rollback copy of the old executable;
- replaces the exact path that initiated the update;
- verifies the installed bytes and relaunches that same path; and
- restores the old executable and writes `.update-error.log` if verification or
  relaunch fails.

This supports both the per-user installer layout and portable copies without
guessing an installation directory. Linux retains its existing verified `.deb`
installation and relaunch path.

## Upgrade notes

Users on v1.2.1 can choose **Update and restart** to install v1.2.2. Because the
update mechanism itself belongs to the old running version, that first upgrade
still begins through the v1.2.1 NSIS flow. Once v1.2.2 is running, later Windows
updates use the exact-path replacement and rollback mechanism described above.
If a machine has multiple old portable copies, remove or archive the obsolete
copies after confirming v1.2.2 is running.

All desktop, CLI, validating, relay, and mining nodes must remain on v1.2.1 or
newer to follow the rule-version-2 main chain activated at block `125555`.
Retain a verified 24-word recovery phrase or `.entwallet` backup before every
upgrade.

## Verification

Release gates cover the complete Go suite and race detector, `go vet`, Windows
and Linux builds, Windows exact-target and rollback regressions, frontend and
website tests, npm audit, reachable vulnerability scanning, Ubuntu package
installation, Secret Service wallet smoke tests, artifact SHA-256 checks, and
GitHub build-provenance attestations.

## 简体中文

Entcoin v1.2.2 是 `entropy-mainnet-v1` 的 Windows 更新可靠性版本。本次不改变
共识、创世块、高度 `125555` 的激活规则、钱包、私钥、地址、余额、交易编码、SQLite
结构、链数据、节点兼容性或现有数据目录。

## Windows 更新修复

旧版 Windows 更新器在安装完成后，总是启动
`%LOCALAPPDATA%\Programs\Entcoin\Entcoin.exe`。如果用户运行的是便携版、改名后的
EXE、自定义路径，或者电脑里同时存在多份 Entcoin，更新完成后就可能打开旧副本。

从 v1.2.2 开始，Windows 更新器下载并校验正式发布的 `Entcoin.exe`，然后由新版程序以
受限 helper 模式完成更新：

- 等待发起更新的旧进程退出；
- 在当前 EXE 所在目录暂存并核对新版文件；
- 备份旧 EXE，再替换发起更新的准确路径；
- 校验替换后的文件，并从同一路径重启；
- 如果替换、校验或重启失败，恢复旧 EXE，并写入 `.update-error.log`。

因此安装版、便携版、改名文件和自定义目录不再依赖写死的默认路径。Linux 继续使用原有的
已校验 `.deb` 安装和重启流程。

## 升级说明

v1.2.1 用户可以点击“更新并重启”安装 v1.2.2。由于第一次升级仍由正在运行的 v1.2.1
旧更新器发起，这一步依然先走旧版 NSIS 流程；成功运行 v1.2.2 后，后续 Windows 升级才会
使用新的原路径替换与失败回滚机制。如果电脑里留有多份旧便携版，请在确认 v1.2.2 正常运行
后删除或归档旧副本。

所有桌面、CLI、验证、转发和挖矿节点都必须保持在 v1.2.1 或更高版本，才能继续跟随已在
高度 `125555` 激活的规则版本 2 主链。每次升级前仍应保留已核验的 24 词恢复短语或
`.entwallet` 备份。

## 验证

发布门禁包括完整 Go 测试与竞态检测、`go vet`、Windows/Linux 构建、Windows 精确路径
替换与失败回滚测试、前端和官网测试、npm audit、可达漏洞扫描、Ubuntu 包安装、Secret
Service 钱包冒烟测试、产物 SHA-256 校验及 GitHub 构建来源证明。
