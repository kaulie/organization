# organization

管理一个实体组织的信息维护，human/agent 管理，角色定位，授权。

本仓库当前实现了**组织架构管理**的第一个切片：部门维护、人员（HUMAN/AGENT）注册与查询。

- 语言/运行时：Go 1.24（仅使用标准库，无第三方依赖）
- 形态：HTTP + JSON 服务；数据以 JSON 文件持久化（`<ORG_DATA_DIR>/org-store.json`，默认 `backend/data`），进程重启/重新发布后数据保留
- 界面：内置单页 Web UI（`embed` 进二进制，打开 `/` 即用，见「Web UI（浏览器界面）」）

## 功能与需求对应

| 需求 | 实现 |
| --- | --- |
| 1. 新增部门（部门名称、部门类型：研发/测试/产品/管理） | `POST /api/v1/departments` |
| 2. 新增人员注册（名称、ID、类型 HUMAN/AGENT、所在部门，注册后生成工号） | `POST /api/v1/persons` |
| 3. 查看部门信息及下属人员列表 | `GET /api/v1/departments/{id}` |
| 4. 查看人员信息 | `GET /api/v1/persons/{id}`（支持人员 ID 或工号） |
| 5. 部门重命名（保留部门 ID、类型与下属人员） | `PATCH /api/v1/departments/{id}` |

## 快速开始

```bash
make run                 # 默认监听 :8080，可用 SERVICE_PORT / ORG_ADDR 覆盖（见「环境变量」）
# 或者
go run ./cmd/server
```

端到端演示（自动创建部门、注册人员并查询）：

```bash
make demo                # 等价于 ./examples/demo.sh
```

### 使用示例

```bash
# 1) 新增部门，类型可用中文或英文别名（研发/rd/dev、测试/qa、产品/product、管理/admin）
curl -s -X POST localhost:8080/api/v1/departments \
  -H 'Content-Type: application/json' \
  -d '{"name":"研发中心","type":"研发"}'
# => {"id":"D0001","name":"研发中心","type":"研发","createdAt":"..."}

# 2) 注册人员（HUMAN / AGENT），成功后返回系统分配的工号
curl -s -X POST localhost:8080/api/v1/persons \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","id":"user-001","type":"HUMAN","departmentId":"D0001"}'
# => {"id":"user-001","name":"Alice","type":"HUMAN","departmentId":"D0001","employeeNo":"E0001","departmentName":"研发中心","createdAt":"..."}

curl -s -X POST localhost:8080/api/v1/persons \
  -H 'Content-Type: application/json' \
  -d '{"name":"Cline","id":"agent-001","type":"AGENT","departmentId":"D0001"}'

# 3) 查看部门信息 + 下属人员列表
curl -s localhost:8080/api/v1/departments/D0001

# 4) 查看人员信息（人员 ID 或工号均可）
curl -s localhost:8080/api/v1/persons/user-001
curl -s localhost:8080/api/v1/persons/E0001

# 5) 部门重命名（只改名称，ID / 类型 / 下属人员保持不变）
curl -s -X PATCH localhost:8080/api/v1/departments/D0001 \
  -H 'Content-Type: application/json' \
  -d '{"name":"平台研发部"}'
# => {"id":"D0001","name":"平台研发部","type":"研发","createdAt":"..."}
```

## Web UI（浏览器界面）

服务内置一个单页界面（Go `embed` 打进二进制，无第三方依赖、无单独前端构建步骤），
浏览器打开根路径即可使用：

```
http://localhost:8080/          # 单页界面
```

界面能力（全部通过同源 JSON API `/api/v1/*` 读写）：

- 顶部状态条：探活 `/health` 并显示当前部署版本。
- 概览统计：部门数、人员总数、HUMAN / AGENT 分布。
- 新增部门（名称 + 类型）与人员注册（名称 / 人员 ID / 类型 / 所在部门）。
- 部门列表（含成员数，点击可展开部门详情抽屉查看成员）与人员列表（按姓名 / 人员 ID / 工号搜索，按部门筛选）。
- 部门重命名：列表中每行的「重命名」按钮（或详情抽屉里的同名按钮）弹出对话框，改完后两个列表与抽屉自动刷新。

路由约定：

| 路径 | 说明 |
| --- | --- |
| `/` | 单页界面（精确匹配，不会遮蔽 API 路由） |
| `/ui/*` | 界面静态资源（`styles.css`、`app.js`） |

> 因为界面资源嵌在二进制里，部署平台只分发 `bin/orgd` + `scripts/` 也能正常渲染，
> 不依赖可执行文件旁边的任何前端文件。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVICE_PORT` | `8080` | 监听端口。只设置它时监听 `:SERVICE_PORT`（所有网卡） |
| `ORG_ADDR` | — | 完整监听地址，如 `127.0.0.1:4250`。**优先级高于 `SERVICE_PORT`** |
| `ORG_BIND` | `127.0.0.1` | 仅由 `scripts/start.sh` 使用：决定绑定哪张网卡，最终地址为 `${ORG_BIND}:${SERVICE_PORT}` |
| `APP_VERSION` | `dev` | 版本号，出现在 `/health` 响应与启动/关闭日志中；发版时由平台注入，二进制内以 `-ldflags` 兜底 |
| `ORG_DATA_DIR` | `backend/data` | 数据目录，存储文件为 `<ORG_DATA_DIR>/org-store.json`。部署时 `scripts/start.sh` 注入 `…/backend/data`，该目录由部署器跨发布保留（见「数据持久化」） |

优先级：**`ORG_ADDR` > `SERVICE_PORT` > 内置默认 `8080`**。

```bash
SERVICE_PORT=9000 go run ./cmd/server                  # 监听 :9000（所有网卡）
ORG_ADDR=127.0.0.1:9000 go run ./cmd/server            # 只监听本机 9000（覆盖 SERVICE_PORT）
make run SERVICE_PORT=9000                             # 同上，走 Makefile
```

> **为什么不用通用的 `PORT`？** 同一台主机上往往已有别的运行时导出了 `PORT`
> （本机实测 `PORT=4211` 来自共存的 web-cursor 部署），直接继承会让本服务被顶到
> 别人的端口上、形成难查的串扰。因此服务只认 `SERVICE_PORT`/`ORG_ADDR`。
> 部署链路由 `scripts/start.sh` 显式桥接：控制面按服务契约 `healthUrl` 注入 `PORT`，
> 脚本再映射为 `SERVICE_PORT` 与 `ORG_ADDR="${ORG_BIND:-127.0.0.1}:${SERVICE_PORT}"`，
> 端口始终跟随契约，并默认只绑本机回环。若端口被占用，进程会以
> `bind: address already in use` 退出，`start.sh` 据此返回 1 并打印日志尾部。

## API 参考

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 健康检查（部署平台统一探活路径，返回 `status`/`service`/`version`） |
| `GET` | `/healthz` | 同上，兼容别名 |
| `POST` | `/api/v1/departments` | 新增部门 |
| `GET` | `/api/v1/departments` | 部门列表（附带可选类型枚举 `types`） |
| `GET` | `/api/v1/departments/{id}` | 部门详情，含 `members` 下属人员列表 |
| `PATCH` | `/api/v1/departments/{id}` | 部门重命名（仅改名称，保留 ID/类型/成员） |
| `POST` | `/api/v1/persons` | 人员注册，返回分配的工号 |
| `GET` | `/api/v1/persons` | 人员列表，支持 `?departmentId=D0001` 过滤 |
| `GET` | `/api/v1/persons/{id}` | 人员详情，`{id}` 可为人员 ID 或工号 |

### 请求字段

`POST /api/v1/departments`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 部门名称，1–64 字符，全局唯一（忽略大小写与首尾空白） |
| `type` | 是 | 部门类型：`研发` / `测试` / `产品` / `管理`，也接受 `rd`、`dev`、`qa`、`test`、`product`、`pm`、`mgmt`、`admin` 等别名 |

`PATCH /api/v1/departments/{id}`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 新部门名称，校验与唯一性规则同创建（1–64 字符，忽略大小写与首尾空白，不能与其他部门重名） |

> 重命名只改 `name`：`id`、`type`、`createdAt` 与下属人员都不变，因此外部持有的部门 ID 始终有效；
> 重命名为自己当前的名字（含大小写/空白差异）视为幂等成功，不报冲突。

`POST /api/v1/persons`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 人员名称，1–64 字符 |
| `id` | 是 | 人员 ID，1–64 字符，全局唯一（外部系统标识，由调用方指定） |
| `type` | 是 | `HUMAN` 或 `AGENT`（忽略大小写） |
| `departmentId` | 是 | 所属部门 ID，必须已存在 |

### 响应形状

- 成功：直接返回资源对象（创建返回 `201 Created`）。
- 列表：`{"items":[...],"types":[...]}`。
- 失败：`{"error":{"kind":"validation|not_found|conflict|internal","message":"..."}}`

| kind | HTTP 状态码 |
| --- | --- |
| `validation` | 400 |
| `not_found` | 404 |
| `conflict` | 409 |
| `internal` | 500 |

### 标识生成规则

- 部门 ID：`D` + 4 位序号，如 `D0001`。
- 工号：`E` + 4 位序号，如 `E0001`，按注册顺序分配，全局唯一且不重复。

## 设计说明

```
build.sh            发版构建（产出 outputs/，供部署平台 release.sh 调用）
scripts/            标准启停脚本（start/stop/restart，见“部署与启停”）
cmd/server          HTTP 服务入口（配置、优雅退出）
internal/httpapi    传输层：路由、JSON 编解码、错误到 HTTP 状态码的映射
internal/httpapi/webui  内置单页界面（go:embed，经 /ui/* 提供静态资源）
internal/org        领域层：模型与校验、业务用例 Service、存储 Store（内存实现 + JSON 文件持久化包装）
```

- **分层动机**：`Store` 是接口，当前提供线程安全的 JSON 文件实现（内存工作集 + 原子落盘）；后续可替换为 SQLite/MySQL/远程服务而不影响用例与 API。
- **一致性与并发**：`memoryStore` 用读写锁保护，创建部门的名称唯一性检查、ID/工号分配、创建人员的 ID 唯一性检查都在同一把锁内完成，避免竞态与重复工号。
- **校验前移**：字段格式校验在 `Service`，唯一性等需要读取全局状态的校验在 `Store` 内加锁完成。
- **持久化**：`org.NewFileStore` 在内存 store 之上把每次写操作镜像到 JSON 文件，写出采用「临时文件 + `fsync` + `rename`」原子替换；写入失败会回滚内存状态并返回 `500`，避免「接口报成功、重启后数据消失」。数据文件损坏或来自更新的 schema 时进程**拒绝启动**（绝不静默当成空数据）。
- **类型规范化**：`研发/测试/产品/管理` 为规范存储值，英文别名在入口处归一化，便于对外友好、对内稳定。

## 部署与启停（deployment 平台标准）

服务按 `/Users/gaolei/deployment` 发版工具的约定交付：仓库根目录提供 `build.sh`，运行时由 `scripts/{start,stop,restart}.sh` 接管。

### 发版包约定

```
build.sh                    仓库根目录，release.sh 调用它产出 outputs/
scripts/start.sh            启动（含探活）
scripts/stop.sh             停止（TERM → 15s → KILL）
scripts/restart.sh          重启（stop + start，平台 restartCmd）
```

`APP_VERSION`（8 位短 hash）由发版工具注入，`build.sh` 同时用
`-ldflags -X main.version=$APP_VERSION` 烧进二进制，运行期优先读环境变量，
因此 `/health` 里的 `version` 始终等于当前部署版本。

```bash
/Users/gaolei/deployment/bin/release.sh organization main   # 产出发版包
/Users/gaolei/deployment/bin/deploy.sh  organization deployment-<hash>
```

`release.sh` 与 `deploy.sh` 都会强校验 `outputs/scripts/restart.sh` 是否存在。

### runtime 布局

```
/Users/gaolei/runtime/organization/
├── bin/orgd                可执行文件（来自发版包）
├── scripts/*.sh            启停脚本（来自发版包）
├── VERSION / DEPLOYMENT / COMMIT / GIT_REPO_URL
└── backend/                ← deploy.sh rsync --delete 时保留
    ├── .env                可选覆盖项，首次启动自动生成（权限 600，不入 git）
    ├── data/               数据目录（org-store.json，跨发布保留）
    ├── runtime.pid         进程号
    └── server.log          标准输出/错误
```

### 启停脚本契约

控制面以 `restartCmd` 调用 `scripts/restart.sh`，`cwd = runtimeDir`，并注入：

| 变量 | 含义 |
| --- | --- |
| `PORT` | 服务契约 `healthUrl` 的端口（平台统一通用名）。脚本把它映射为 `SERVICE_PORT`，并绑定 `127.0.0.1:${SERVICE_PORT}`（默认 `4250`） |
| `RUNTIME_DIR` | runtime 根目录 |
| `APP_VERSION` | 本次部署的 8 位短 hash |

`start.sh` 的行为：缺失二进制直接报错退出；首次启动生成 `backend/.env`；
已在运行则跳过（幂等）；`nohup` 拉起后写 `backend/runtime.pid`；轮询
`http://127.0.0.1:${SERVICE_PORT}/health` 最多 20s，成功才返回 0，失败则打印日志尾部、
清理 pid 文件并返回 1。`stop.sh` 在未运行时返回 0（幂等），确保 `deploy.sh`
的重启链路不会因为空跑而失败。

手动运行（默认端口 `4250`）：

```bash
RUNTIME_DIR=/Users/gaolei/runtime/organization PORT=4250 \
  bash scripts/restart.sh
```

### 数据持久化

存储是 JSON 文件：`${ORG_DATA_DIR}/org-store.json`（默认 `backend/data/org-store.json`）。

- 部署时 `scripts/start.sh` 注入 `ORG_DATA_DIR=${RUNTIME_DIR}/backend/data`，而 `deploy.sh` 以
  `--filter='P backend/data/'` 保留该目录 —— 所以**重新发布（= 重启进程）不会丢数据**。
- 每次写操作都镜像到磁盘；写出是「临时文件 + `fsync` + `rename`」的原子替换，进程被强杀也不会留下半个文件。
- 写盘失败会回滚内存状态并返回 `500`（`{"error":{"kind":"internal",...}}`），不会出现「接口说成功、重启后数据没了」。
- 进程启动时会打一行 `store_loaded`（含 `dataFile` 与已加载的部门/人员数），可据此确认数据是否被恢复：

  ```bash
  grep store_loaded ~/runtime/organization/backend/server.log | tail -1
  ```

- 数据文件损坏、无法读取或来自更新的 schema 时，进程**拒绝启动**并打印 `store_open_failed`，
  避免把已有数据静默当成「空数据」。
- 需要换成 SQLite/MySQL：替换 `Store` 实现即可，用例层与 HTTP/API 不受影响。

> 说明：本能力是在「发布后发现数据为空」的问题后补上的。**此前**实例是纯内存存储，
> 那次发布之前录入的数据没有落盘、无法找回；从引入本版本起的发布不会再丢数据。


## 测试

```bash
make test        # go test ./...
make test-race   # go test -race ./...
make cover       # 生成覆盖率
make package     # 产出发版包 outputs/（等价于 ./build.sh）
```

启停脚本已按平台契约实测：首次启动并探活、重复启动幂等跳过、`restart.sh`
换 pid、`stop.sh` 幂等（未运行返回 0）、陈旧 pid 文件自动清理、缺二进制或端口
被占用时返回 1 并打印日志尾部、失败后清理 pid 文件。

覆盖：部门创建（4 种类型 + 别名、校验、重名冲突）、部门重命名（保留 ID/类型/成员、名称索引重建、重名冲突、幂等自改、校验与不存在）、人员注册（工号分配、类型校验、部门存在性、ID 冲突）、部门详情含成员列表、人员查询（按 ID / 按工号）、列表按部门过滤、HTTP 全链路与错误状态码映射、数据文件持久化（跨重启恢复部门/人员/ID 序列、父目录自动创建、空文件视为空库、损坏或高版本文件拒绝启动、写盘失败回滚内存并返回 500）、`ORG_DATA_DIR` 解析。

`examples/demo.sh` 第 7 步会**真实重启进程**（SIGTERM 停 → 重新拉起）并打印重启后的部门与人员，可直接复现「发布后数据不应该为空」。


