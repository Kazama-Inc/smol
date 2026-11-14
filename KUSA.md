# Kazama-Inc/smol：相对上游的差异

这是 `smol-machines/smol` 的 fork，Kusa 桌面端只用其中的 Go SDK（`sdk/go`）。设计依据：`Kazama-Inc/kusa-desktop` 的 `docs/DESIGN.md` 第 10.5 节。

## 分支

| 分支 | 内容 |
|---|---|
| `main` | GitHub fork 时带过来的上游 `main`（`cd966a8`），**原样不动、不作基线**：组织规则集管所有仓库的 `main`，禁止强推、只能走 PR 合入。基线是上游提交 `cd966a8`（「Release smol CLI and SDKs at 1.34.0」之后两条；上游的 Go SDK 1.34.0 没打标签，最新标签是 `sdk/go/v1.33.0`）。上游打出 `sdk/go/v1.34.x` 标签后改用标签作基线 |
| `kusa` | **发布基线**：基线提交加下表的补丁，主仓库引用它，每次合上游后重做（变基或重新合并各 `feat/` 分支） |
| `feat/*` | 每个补丁一个分支，从基线提交拉（不是从 `main`），能单独提给上游 |

主仓库引用：`sdk/go.mod` 里 `replace github.com/smol-machines/smol/sdk/go => github.com/Kazama-Inc/smol/sdk/go <kusa 上的伪版本>`，不改模块路径。

Go SDK 的 Rust 部分按相对路径依赖引擎（`sdk/go/native/Cargo.toml` 的 `path = "../../../../"`），所以本仓库要检出在 `Kazama-Inc/smolvm` 的 `smol/` 目录下。引擎版本见 `Kazama-Inc/smolvm` 的 `KUSA.md`。

## 补丁

目标是这张表只减不增：每条都以 PR 提给上游，合进去一条就删一条。

| 分支 | 内容 | 上游状态 |
|---|---|---|
| `feat/windows-go-sdk` | Go SDK 支持 Windows x64：`native_windows.go`（`LoadLibraryEx` + purego，`SetDefaultDllDirectories` 收紧 DLL 搜索）、`cleanup_windows.go`（控制台事件优雅停机 + `KILL_ON_JOB_CLOSE` 作业对象兜底回收）、`check_host` 的 WHP 探测（`LocalAvailability.Cause`）、`windows-x64` 运行时包（`assets.go`、`bundle-runtime.sh`）、运行时包符号链接越界检查修复、Windows 上失败的 3 个单测、`e2e_windows_test.go` | 暂不提上游 |
| `feat/local-image-archive` | `MachineConfig.Image` 是本地 `docker save` 归档或 rootfs 目录时，在主机上解析（与 CLI 的 `--image` 同一调用），不联网也能建机。与平台无关 | 暂不提上游 |
| `feat/block-private` | `ResourceSpec.BlockPrivate` / `AllowPrivate`，接到引擎的 `block_private`。与 CLI 不同，不会自己打开网络 | 暂不提上游 |
| `feat/windows-state-dir`（叠在 `feat/windows-go-sdk` 上） | `CheckLocal` 第 4 步：引擎状态目录的 AF_UNIX 自检，失败时 `Cause: state_dir_unusable`；README 写明 `SMOLVM_DATA_DIR`。依赖 smolvm 的 `feat/windows-state-dir` | 暂不提上游 |
| `feat/windows-escape-tests` | 越权集里挂载目录的部分（junction、文件名里的 `\` 和盘符、主机符号链接）作 Windows e2e。修复在 libkrun fork | 暂不提上游 |
| `feat/windows-workspace-owner-test` | Windows e2e：非 root 工作负载能写 `/workspace`（uid 1000 直接写；机器用户 1001 走 id 映射）。修复在 smolvm `feat/windows-share-host-owner` 和 libkrun `feat/windows-virtiofs-host-owner` | 暂不提上游 |
| `fix/e2e-heartbeat-counter` | e2e 的心跳改成计数器：busybox 的 `date` 不支持 `%N`，`date +%s%N` 只输出整秒，`beating()` 相隔 700 ms 的两次读数约三成相同，`TestE2EExecControl` 因此约三成误报「streamed command is not running before cancel」。与平台、`krun.dll` 编法无关 | 暂不提上游 |

## 构建与测试（Windows）

```bash
# 引擎检出在 D:\smol-fork\smolvm，本仓库在它的 smol/ 下
cd smol/sdk/go/native
RUSTUP_TOOLCHAIN=stable-x86_64-pc-windows-msvc cargo build --release   # -> smol_go.dll
cd .. && GOARCH=amd64 CGO_ENABLED=0 go test ./...                       # 单测
SMOL_E2E=1 SMOLMACHINES_RUNTIME_DIR=<运行时目录> SMOLMACHINES_NO_DOWNLOAD=1 \
  SMOL_E2E_IMAGE=<docker save 的 tar> GOARCH=amd64 go test -run E2E ./... # 起虚拟机，要 WHP
```

- `rust-toolchain.toml` 写的是 `stable`，在缺省主机为 gnu 的机器上会选到 gnu 工具链；一律用 MSVC。
- 从 MSIX 应用（例如 Claude 桌面端）里起的进程继承 AppData 重定向，`%LOCALAPPDATA%\smolvm` 下 AF_UNIX 连不上。设 `SMOLVM_DATA_DIR` 到 AppData 以外的短目录（例如 `%TEMP%` 下）；没设时 `CheckLocal` 报 `state_dir_unusable`，建机 2 秒内失败并说明原因。
- 补丁暂不提上游（11-14 用户决定）。上游三个仓库的 CI 都有 `no-ai-attribution` 检查，会拒绝带 AI 署名（`Co-Authored-By: … anthropic.com`）的提交，将来要提时另行处理。
