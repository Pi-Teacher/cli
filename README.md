# pi-teacher-cli

Pi Teacher 的远程业务 CLI, 面向用户与 AI agent. 它是
[pi-teacher-server](https://github.com/Pi-Teacher/server) REST API 的命令行封装:
只通过服务端 `/api/cli/...` 接口交互, 认证使用服务端地址与 API Key, 永不直接连接数据库.

CLI 覆盖知识库与复习的核心业务: Topic / Card / Glossary 的增查改回收,
卡片查重与合并, 回收站读取与恢复, FSRS 复习调度, 用户画像与审批查询.
永久删除, Embedding 管理, 系统设置等能力属于 WebUI, 不在本 CLI 范围.

## 安装

从 [Releases](https://github.com/Pi-Teacher/cli/releases) 下载对应平台的二进制,
文件名为 `{platform}-pi-teacher-cli-{version}`, platform 覆盖
`linux/darwin/windows` 的 `amd64/arm64` 共六种组合. 下载后去掉执行限制即可使用:

```bash
chmod +x linux-amd64-pi-teacher-cli-v1.0.0
./linux-amd64-pi-teacher-cli-v1.0.0 version
```

也可以从源码构建, 需要 Go 1.26.7:

```bash
git clone https://github.com/Pi-Teacher/cli.git
cd cli
make build    # 产物在 ./bin/pi-teacher-cli
```

## 配置

首次使用前配置服务端地址与 API Key (在 WebUI 的 API Keys 页面创建):

```bash
pi-teacher-cli config set-server https://your-server.example
pi-teacher-cli config set-api-key ptk_xxxxxxxxxxxx
pi-teacher-cli config show
```

配置保存在 `~/.config/pi-teacher/config.json`, 目录权限 `0700`, 文件权限 `0600`,
输出时 API Key 默认脱敏. 也可以用环境变量 `PI_TEACHER_SERVER_URL` 与
`PI_TEACHER_API_KEY` 代替配置文件, 优先级: 命令行参数 > 环境变量 > 配置文件.

## 用法

命令按业务分组, 每个命令支持 `--json` 输出机器可读结构, 默认为人类可读格式:

```bash
pi-teacher-cli system info                        # 服务端版本与运行状态
pi-teacher-cli topic list --q 算法                # Topic 列表 (分页/搜索)
pi-teacher-cli card list --topic-id 0             # 无 Topic 的卡
pi-teacher-cli card check --front "新问题"         # 建卡前查重 (精确+语义)
pi-teacher-cli card create --front "问题" --back "答案"
pi-teacher-cli card merge 12 34 --no-embedding   # 合并重复卡
pi-teacher-cli review due                         # 到期复习队列
pi-teacher-cli review submit 12 --rating good --expected-card-version 3 --expected-schedule-version 2
pi-teacher-cli trash cards list                   # 回收站
pi-teacher-cli approvals list --status pending    # 查看自己发起的提案
```

完整命令清单运行 `pi-teacher-cli` 不带参数查看.

### 修改类命令的约定

所有修改类命令必须提交 `expected_version` 乐观锁版本号 (取自对应 `get`
或列表输出的 `version` 字段), 数据被并发修改时返回 409 并提示重新读取,
CLI 不会自动覆盖或自动重试. 所有写命令自动携带 `Idempotency-Key`,
网络超时后重试是安全的.

服务端审批开关开启时, 写命令返回 202 提案 (输出会明确标注"未生效"),
需要用户在 WebUI 审批后才真正执行; 审批开关关闭时直接生效.
两种结果在输出与 `--json` 信封 (`outcome: proposal / applied`) 中严格区分.

### 退出码

| 退出码 | 含义 |
|--------|------|
| 0      | 成功 |
| 1      | 一般错误 (网络故障, 未知错误码) |
| 2      | 命令行用法错误 |
| 3      | 认证失败 (API Key 无效) |
| 4      | 乐观锁版本冲突 |
| 5      | 无权限 |
| 6      | 对象不存在 |
| 7      | 请求被限流 |
| 8      | embedding 服务不可用 |
| 9      | 语义相似度检测未开放 |
| 10     | 合并需要显式指定 Topic |
| 11     | 合并需要显式指定 embedding |

## 开发

```bash
make build        # 构建到 ./bin/pi-teacher-cli
make test         # 全部测试
make test-race    # 竞态检测
make lint         # gofmt + go vet
make staticcheck  # staticcheck
```

标准库优先, 零第三方运行时依赖. 发布为纯手动 GitHub Action:
打 tag 推送后在 Actions 页面触发 Release, 交叉编译六个平台产物.

## License

本项目基于 [MIT License](LICENSE) 开源。

Copyright (c) 2026 Pi-Teacher
