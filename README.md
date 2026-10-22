# organization

管理一个实体组织的信息维护，human/agent 管理，角色定位，授权。

本仓库当前实现了**组织架构管理**的第一个切片：部门维护、人员（HUMAN/AGENT）注册与查询。

- 语言/运行时：Go 1.24（PostgreSQL 驱动 `jackc/pgx`；其余仍是标准库）
- 形态：HTTP + JSON 服务。配置了 `ORG_DATABASE_URL` 时数据在 PostgreSQL（`49.234.45.173` 上的 `organization` 库）；未配置时仍用 JSON 文件（`<ORG_DATA_DIR>/org-store.json`）。第一次连上空库时，会把已有的 JSON 快照导入，避免把线上部门目录换成空库。
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
| `ORG_DATA_DIR` | `backend/data` | JSON 快照目录（`<ORG_DATA_DIR>/org-store.json`）。没设 `ORG_DATABASE_URL` 时它就是存储；设了之后它只作为空库的一次性导入来源。部署时 `scripts/start.sh` 注入 `…/backend/data`，该目录跨发布保留 |
| `ORG_DATABASE_URL` | 空 | PostgreSQL 连接串。部署由平台 `runtimeEnv` 注入，指向 `49.234.45.173:5432/organization`（`sslmode=require`）。口令不进仓库。未设置时使用 JSON 文件 |
| `REGISTRY_URL` | `http://127.0.0.1:4240` | 服务中心。`Listen` 成功后、开始接请求之前 `PUT .../announce` 上报本进程地址；失败则进程退出 |
| `REGISTRY_NS` | `default` | 上报用的命名空间，须与已登记契约一致 |
| `SERVICE_NAME` | `organization` | 上报用的服务名，即部署平台的 `serviceId` |
| `REGISTRY_TOKEN` | 空 | 写接口收紧后的 `Authorization: Bearer` 令牌。当前写接口开放时不需要 |

优先级：**`ORG_ADDR` > `SERVICE_PORT` > 内置默认 `8080`**。

进程只在启动时上报一次，退出时按响应里的 `instance.id` 摘掉这条地址。这不是心跳：注册中心没有 TTL。`darwin` 会上报成 `mac`。监听在具体网卡（部署时是 `127.0.0.1`）时带上 `host`；监听 `0.0.0.0` / `::` 时省略 `host`，由注册中心记下这次连接的对端地址。规范见 [service-registry docs/register.md](https://github.com/kaulie/service-registry/blob/main/docs/register.md)。

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

## 服务契约与自动登记（注解是唯一真源）

本服务的对外契约**不是手写的规范文件，而是代码里的注解**：`swag init` 读注解生成
OpenAPI，再由服务中心的脚本上报。接口改了，注解跟着改，下一次发布/CI 契约自动刷新 ——
没有人需要手工贴 spec。

```
cmd/server/main.go 顶部（General API Info）
internal/httpapi/handler.go 每个 handler 一段注解
        │
        ├─ swag init -g cmd/server/main.go -o docs --outputTypes json   →  docs/swagger.json（产物，不提交）
        └─ scripts/register-contract.sh  →  服务中心 client/ci/register-go-service.sh  →  PUT 契约 + 实例
```

注解长这样（`internal/httpapi/handler.go`，只影响生成规范，**运行期零依赖**，服务不 import swaggo 任何包）：

```go
// @Summary  部门重命名
// @Tags     departments
// @Param    id       path      string                       true  "部门 ID，如 D0001"
// @Param    request  body      org.RenameDepartmentRequest  true  "新名称"
// @Success  200      {object}  org.Department
// @Router   /api/v1/departments/{id} [patch]
```

### 触发点

| 触发点 | 位置 | 说明 |
| --- | --- | --- |
| 发版 | `build.sh` 末尾 | 发版流程跑在本机，而服务中心只绑 `127.0.0.1:4240`，所以这里是主要触发点 |
| CI | `.github/workflows/register-contract.yml` | 需 **self-hosted runner**（能直连服务中心）；默认只手工触发，runner 就绪后打开 `push: branches: [main]` |
| 手工 / 补登记 | `make register` 或 `bash scripts/register-contract.sh` | 幂等，随时可跑 |

登记是**幂等**的：规范原文的 sha256 与库里一致时跳过 PUT（不刷 revision，不产生无意义的变更记录）。
这一步**默认不改实例**。监听地址由进程在 `Listen` 成功后自己 `PUT .../announce`；`register.sh` 在没有 `--instance` 时会跳过实例同步。只有显式设置 `INSTANCES` 才做声明式整组对齐，列表里没有的地址会被删掉，所以进程已经上报之后不要再用一份不含那条地址的列表去同步。

### 参数（都可用环境变量覆盖）

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVICE_NAME` | `organization` | 注册用的服务名 |
| `DEPARTMENT_ID` | `D0006` | 归属部门（IT组织部）。查目录：`curl http://127.0.0.1:4240/v1/departments` |
| `INSTANCES` | 空 | 默认不传。空则不碰实例（地址由进程启动时上报）。显式设置后按逗号分隔做声明式整组对齐，列表外的地址会被删掉 |
| `REGISTRY_URL` | `http://127.0.0.1:4240` | 服务中心地址（只绑本机回环） |
| `REGISTRY_NS` / `REGISTRY_TOKEN` | `default` / 空 | 命名空间与写令牌（写接口收紧后才需要，走密钥管理，别写进仓库） |
| `VERSION` | `$APP_VERSION` 或 `git describe` | 上报的版本号；发版时即本次部署的 8 位短 hash。**只在契约真的变化时才会写进库里**：`register.sh` 比对规范原文 sha256 一致就跳过整个 PUT（避免把变更表刷成噪声），所以纯代码重构不会改动注册中心里的 `version` |
| `OWNER` / `HEALTH_PATH` / `TAGS` / `GIT_REPO` | `kaulie` / `/health` / `org` / 本仓库地址 | 契约元数据 |
| `SKIP_REGISTER_CONTRACT=1` | 关 | `build.sh` 里跳过登记 |
| `REGISTER_CONTRACT_STRICT=1` | 关 | `build.sh` 里把登记失败从「告警」改成「让发版失败」 |

服务中心的客户端脚本（`client/ci/register-go-service.sh`、`client/register.sh`）在
[service-registry](https://github.com/kaulie/service-registry) 仓库里；`scripts/register-contract.sh`
按「环境变量指定 → 本地检出 → 浅克隆兜底」的顺序找到它，所以服务仓库不需要 vendor 一份副本。

```bash
# 本地看一眼注解生成了什么（不上报）
make contract-swagger
python3 -c 'import json;d=json.load(open("docs/swagger.json"));print(list(d["paths"]))'

# 登记（幂等）并回读校验
make register
curl -s http://127.0.0.1:4240/v1/namespaces/default/services/organization |
  python3 -c 'import json,sys;s=json.load(sys.stdin)["service"];print(s["departmentName"],len(s["api"]["endpoints"]),"个端点")'
```

## 设计说明

```
build.sh            发版构建（产出 outputs/，供部署平台 release.sh 调用；末尾登记服务契约）
scripts/            标准启停脚本（start/stop/restart，见“部署与启停”）+ register-contract.sh（契约登记）
.github/workflows/  CI（register-contract.yml：注解 → OpenAPI → 登记，需 self-hosted runner）
cmd/server          HTTP 服务入口（配置、优雅退出）+ swag General API Info（注解入口）
internal/registry   启动后向服务中心 announce 本进程地址，退出时按 instance.id 摘掉
internal/httpapi    传输层：路由、JSON 编解码、错误到 HTTP 状态码的映射、契约响应类型与注解
internal/httpapi/webui  内置单页界面（go:embed，经 /ui/* 提供静态资源）
internal/org        领域层：模型与校验、业务用例 Service、存储 Store（内存、JSON 文件、PostgreSQL）
```

- **契约即注解**：对外契约由 `cmd/server/main.go`（General API Info）与 `internal/httpapi/handler.go`
  （每个 handler 的 `@Summary/@Param/@Success/@Router`）生成，运行期零依赖；`route` 表既是 mux 的唯一来源，
  也被 `contract_test.go` 拿来与注解逐条比对，防止「路由改了注解没改」（会向注册中心谎报接口）。

- **分层动机**：`Store` 是接口。部署使用 PostgreSQL；本机没有连接串时仍用 JSON 文件（内存工作集 + 原子落盘）。用例层与 HTTP/API 不依赖具体存储。
- **一致性与并发**：内存实现用读写锁把名称唯一性、ID/工号分配包在同一次操作里。PostgreSQL 把同样的检查、发号和插入放在一个事务里，名称与工号用唯一约束兜底。
- **校验前移**：字段格式校验在 `Service`，唯一性等需要读取全局状态的校验在 `Store` 内完成。
- **持久化**：设置 `ORG_DATABASE_URL` 后读写都走 PostgreSQL。库还是空的、并且 `org-store.json` 能读时，启动会把这份快照导入一次（部门、人员、发号序列一起提交）；库里已经有部门则不再导入。JSON 文件损坏且库仍为空时进程拒绝启动。未设置连接串时仍用 `org.NewFileStore`：写出采用「临时文件 + `fsync` + `rename`」，写入失败回滚内存并返回 `500`。
- **类型规范化**：`研发/测试/产品/管理` 为规范存储值，英文别名在入口处归一化，便于对外友好、对内稳定。

## 部署与启停（deployment 平台标准）

服务按 `/Users/gaolei/deployment` 发版工具的约定交付：仓库根目录提供 `build.sh`，运行时由 `scripts/{start,stop,restart}.sh` 接管。

### 发版包约定

```
build.sh                    仓库根目录，release.sh 调用它产出 outputs/；末尾读注解登记服务契约
scripts/start.sh            启动（含探活）
scripts/stop.sh             停止（TERM → 15s → KILL）
scripts/restart.sh          重启（stop + start，平台 restartCmd）
scripts/register-contract.sh  注解 → OpenAPI → 登记服务中心（发版与 CI 复用，见「服务契约与自动登记」）
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
    ├── data/               历史 JSON 快照（org-store.json，跨发布保留；空库时导入 PostgreSQL）
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
清理 pid 文件并返回 1。进程在开始接请求之前会向服务中心上报监听地址，上报失败
则进程退出，探活随之失败。`stop.sh` 发 SIGTERM 后进程先摘掉自己的实例再关闭
HTTP；`stop.sh` 在未运行时返回 0（幂等），确保 `deploy.sh` 的重启链路不会因为空跑而失败。

手动运行（默认端口 `4250`）：

```bash
RUNTIME_DIR=/Users/gaolei/runtime/organization PORT=4250 \
  bash scripts/restart.sh
```

### 数据持久化

部署后的存储是 PostgreSQL：`ORG_DATABASE_URL` 由部署平台的 `runtimeEnv` 注入，`scripts/start.sh` 让这个值优先于 `backend/.env`。库在 `49.234.45.173:5432`，库名 `organization`，连接使用 `sslmode=require`。口令只放在平台运行时环境里，不进 git。

此前的存储是 JSON 文件 `${ORG_DATA_DIR}/org-store.json`（不是 SQLite）。第一次用空的 PostgreSQL 启动时，进程会导入这份文件里的部门、人员和发号序列，然后继续从原序列往后编号。库里已经有部门时不再导入，所以重启不会把同一份快照写两遍。

- `deploy.sh` 仍以 `--filter='P backend/data/'` 保留 `backend/data/`，因此导入来源在发布后还在。
- `/health` 会 ping 数据库；连不上时返回 503，部署探活失败，不会把一个空进程当成上线成功。
- 进程启动时会打一行 `store_loaded`（含部门/人员数）和 `store_opened`（`engine=postgres`，以及是否 `importedFromFile`）：

  ```bash
  grep store_loaded ~/runtime/organization/backend/server.log | tail -1
  ```

- 连接失败、或空库遇到损坏的 JSON 快照时，进程拒绝启动并打印 `store_open_failed` / `store_import_failed`。
- 未设置 `ORG_DATABASE_URL`（本地 `make run`）时仍写 JSON 文件：临时文件 + `fsync` + `rename`，写盘失败回滚并返回 500。

> 说明：本能力是在「发布后发现数据为空」的问题后补上的。**此前**实例是纯内存存储，
> 那次发布之前录入的数据没有落盘、无法找回；从引入本版本起的发布不会再丢数据。


## 测试

```bash
make test                 # go test ./...
make test-race            # go test -race ./...
make cover                # 生成覆盖率
make package              # 产出发版包 outputs/（等价于 ./build.sh，末尾会登记契约）
make contract-swagger     # 只从注解生成 docs/swagger.json（不上报，本地看契约长什么样）
make register             # 注解 → OpenAPI → 登记到服务中心（幂等）
```

启停脚本已按平台契约实测：首次启动并探活、重复启动幂等跳过、`restart.sh`
换 pid、`stop.sh` 幂等（未运行返回 0）、陈旧 pid 文件自动清理、缺二进制或端口
被占用时返回 1 并打印日志尾部、失败后清理 pid 文件。

覆盖：部门创建（4 种类型 + 别名、校验、重名冲突）、部门重命名（保留 ID/类型/成员、名称索引重建、重名冲突、幂等自改、校验与不存在）、人员注册（工号分配、类型校验、部门存在性、ID 冲突）、部门详情含成员列表、人员查询（按 ID / 按工号）、列表按部门过滤、HTTP 全链路与错误状态码映射、数据文件持久化（跨重启恢复部门/人员/ID 序列、父目录自动创建、空文件视为空库、损坏或高版本文件拒绝启动、写盘失败回滚内存并返回 500）、`ORG_DATA_DIR` 解析、**契约一致性**（`internal/httpapi/contract_test.go` 逐条比对 `@Router` 注解与路由表：注解与路由任何一边多了/少了都失败；`cmd/server` 断言 General API Info 仍在 `package main` 之前的注解块里）。

契约链路：`make contract-swagger` 从注解生成 6 个路径 / 9 个操作（含 `/health`、`/healthz`、`/api/v1/{departments,persons}` 的 7 个业务操作与全部请求/响应定义）。`make register` 只刷新契约，不改实例集合；重复执行在规范没变时输出「契约无变化…跳过登记」且不产生新的 revision。监听地址由进程启动时 `announce`，退出时按 `instance.id` 删除。`build.sh` 末尾同样只登记契约。

`examples/demo.sh` 第 7 步会**真实重启进程**（SIGTERM 停 → 重新拉起）并打印重启后的部门与人员，可直接复现「发布后数据不应该为空」。


