# organization

管理一个实体组织的信息维护，human/agent 管理，角色定位，授权。

本仓库当前实现了**组织架构管理**的第一个切片：部门维护、人员（HUMAN/AGENT）注册与查询。

- 语言/运行时：Go 1.24（仅使用标准库，无第三方依赖）
- 形态：HTTP + JSON 服务，内存存储（进程重启后数据清空）

## 功能与需求对应

| 需求 | 实现 |
| --- | --- |
| 1. 新增部门（部门名称、部门类型：研发/测试/产品/管理） | `POST /api/v1/departments` |
| 2. 新增人员注册（名称、ID、类型 HUMAN/AGENT、所在部门，注册后生成工号） | `POST /api/v1/persons` |
| 3. 查看部门信息及下属人员列表 | `GET /api/v1/departments/{id}` |
| 4. 查看人员信息 | `GET /api/v1/persons/{id}`（支持人员 ID 或工号） |

## 快速开始

```bash
make run                 # 默认监听 :8080，可用 PORT 或 ORG_ADDR 覆盖
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
```

## API 参考

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/healthz` | 健康检查 |
| `POST` | `/api/v1/departments` | 新增部门 |
| `GET` | `/api/v1/departments` | 部门列表（附带可选类型枚举 `types`） |
| `GET` | `/api/v1/departments/{id}` | 部门详情，含 `members` 下属人员列表 |
| `POST` | `/api/v1/persons` | 人员注册，返回分配的工号 |
| `GET` | `/api/v1/persons` | 人员列表，支持 `?departmentId=D0001` 过滤 |
| `GET` | `/api/v1/persons/{id}` | 人员详情，`{id}` 可为人员 ID 或工号 |

### 请求字段

`POST /api/v1/departments`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 部门名称，1–64 字符，全局唯一（忽略大小写与首尾空白） |
| `type` | 是 | 部门类型：`研发` / `测试` / `产品` / `管理`，也接受 `rd`、`dev`、`qa`、`test`、`product`、`pm`、`mgmt`、`admin` 等别名 |

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
cmd/server          HTTP 服务入口（配置、优雅退出）
internal/httpapi    传输层：路由、JSON 编解码、错误到 HTTP 状态码的映射
internal/org        领域层：模型与校验、业务用例 Service、存储 Store
```

- **分层动机**：`Store` 是接口，当前提供线程安全的内存实现；后续可替换为 SQLite/MySQL/远程服务而不影响用例与 API。
- **一致性与并发**：`memoryStore` 用读写锁保护，创建部门的名称唯一性检查、ID/工号分配、创建人员的 ID 唯一性检查都在同一把锁内完成，避免竞态与重复工号。
- **校验前移**：字段格式校验在 `Service`，唯一性等需要读取全局状态的校验在 `Store` 内加锁完成。
- **类型规范化**：`研发/测试/产品/管理` 为规范存储值，英文别名在入口处归一化，便于对外友好、对内稳定。

## 测试

```bash
make test        # go test ./...
make test-race   # go test -race ./...
make cover       # 生成覆盖率
```

覆盖：部门创建（4 种类型 + 别名、校验、重名冲突）、人员注册（工号分配、类型校验、部门存在性、ID 冲突）、部门详情含成员列表、人员查询（按 ID / 按工号）、列表按部门过滤、HTTP 全链路与错误状态码映射。


