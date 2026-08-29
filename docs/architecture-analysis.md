# Immich 服务端架构分析（v3.1.0）与 Go 移植映射

> 本文基于对 [immich-app/immich](https://github.com/immich-app/immich) v3.1.0 源码
> （`server/`、`machine-learning/`、`open-api/immich-openapi-specs.json`）的实际分析整理，
> 是 immich-go 移植工作的依据与对照表。

## 1. 上游服务端总体架构

Immich v3.1.0 服务端（TypeScript / NestJS 11）与广为人知的 1.x 版本差异极大：

| 维度 | 上游实现 |
|---|---|
| 进程模型 | `main.ts` 是监督进程：按 `IMMICH_WORKERS_INCLUDE`（默认 `[api, microservices]`）fork/线程化多个 worker，`api` 监听 HTTP，`microservices` 消费队列 |
| HTTP | Express 5 + NestJS，全局前缀 `/api`，端口 `IMMICH_PORT`（默认 2283） |
| 校验/序列化 | **Zod 4 + nestjs-zod**（非 class-validator），`ZodValidationPipe` / `ZodSerializerInterceptor` |
| ORM | **Kysely + postgres.js**（v3 已移除 TypeORM），schema 用 `@immich/sql-tools` 装饰器生成迁移 |
| 队列 | **BullMQ + Redis**（ioredis），前缀 `immich_bull`，19 个队列、约 60 个 JobName |
| 实时 | socket.io（redis adapter） |
| 媒体 | sharp（缩略图）、exiftool-vendored（元数据）、fluent-ffmpeg（转码/HLS） |
| 向量检索 | pgvector / VectorChord `vector(512)` + HNSW 余弦索引（`clip_index` / `face_index`） |

### 1.1 认证模型（与 1.x 最大的差异之一）

v3.1 **没有 JWT 会话**：

1. `POST /api/auth/login`：bcrypt(cost=10) 校验密码（未知用户用 `LOGIN_DUMMY_HASH` 抹平时间差）；
2. 成功后生成 **32 字节随机不透明 token**（base64 后剥离非 `\w` 字符），服务端只存 **SHA-256 摘要**（`session` 表，bytea）；
3. 响应 201 `LoginResponseDto{accessToken,...}`，并设置 Cookie `immich_access_token`（httpOnly，400 天）等；
4. 请求鉴权优先级（`AuthService.validate`）：共享链接 key/slug → 会话 token（`x-immich-user-token` / `x-immich-session-token` / `?sessionKey` / `Authorization: Bearer` / cookie）→ API Key（`x-api-key` / `?apiKey`，SHA-256 落库 `api_key.key`，带 168 项细粒度 Permission）；
5. 无 refresh-token 端点，仅有 `POST /api/auth/validateToken`。

### 1.2 数据库核心表（snake_case）

- `user`：id/email(unique)/password(bcrypt)/name/isAdmin/storageLabel/quota…（软删除 deletedAt）
- `session`：token bytea(sha256, indexed)/userId/deviceType/deviceOS/appVersion/expiresAt/pinExpiresAt
- `api_key`：key bytea(sha256, indexed)/permissions varchar[]
- `asset`：ownerId/type(IMAGE|VIDEO|AUDIO|OTHER)/originalPath/**checksum bytea(sha1)**/fileCreatedAt/fileModifiedAt/localDateTime/duration/thumbhash/visibility(timeline|archive|hidden|locked)/width/height/livePhotoVideoId/stackId/duplicateId；`UNIQUE(ownerId, checksum)` 部分索引做上传去重
- `asset_exif`：make/model/dateTimeOriginal/latitude/longitude/city…(GiST earthdistance)
- `asset_file`：生成的 rendition（thumbnail/preview/fullside/sidecar/encoded-video）路径注册表
- `album` + `album_asset`（复合主键 join 表）+ albumUsers 共享
- `smart_search` / `face_search`：`vector(512)` CLIP/人脸嵌入
- `system_metadata`、`library`、`person`、`tag_closure`、`*_audit` 等

### 1.3 上传与作业流水线

1. `FileUploadInterceptor`（multer）把文件**直接流式写入** `<media>/upload/<userId>/<id[0:2]>/<id[2:4]>/<uuid>.<ext>`，边写边算 SHA-1；请求头 `x-immich-checksum` 允许上传前查重（`POST /api/assets/bulk-upload-check` 同理）；
2. 建 `asset` 行 → 入队 `AssetExtractMetadata`；
3. microservices 处理：exiftool 提取元数据（宽高、拍摄时间、GPS 反查城市）→ 入队缩略图（sharp：thumbnail 250 / preview 1440，JPEG/WebP + thumbhash）→ 入队 `SmartSearch`（CLIP 图像嵌入）与 `FaceDetection`（检测+识别两段式）→ 存储模板迁移把文件改名进 `<media>/library/...`；
4. `/api/jobs`（legacy）返回 19 个队列的 `jobCounts{active,completed,delayed,failed,paused,waiting}` + `queueStatus{isActive,isPaused}`。

### 1.4 API 面（OpenAPI 3.0，190 路径 / 274 操作）

- 所有路径挂在 `/api` 下；spec 内路径不含 `/api` 前缀；
- 17 个公开端点（login、admin-sign-up、server/ping、version 等），其余三种鉴权方式并列：`bearer` / `cookie` / `api_key`；
- `GET /timeline/bucket` 返回**列式（columnar）DTO**（按字段的平行数组），不是对象数组——移动端强依赖此格式；
- 登录返回 **HTTP 201**；错误体为 Nest 风格 `{message, error, statusCode}`。

## 2. immich-machine-learning 服务接口（兼容核心）

`machine-learning/` 是独立 Python/FastAPI 容器（`immich_ml`），对服务端只暴露三个 HTTP 端点：

```
GET  /            → {"message":"Immich ML"}
GET  /ping        → 200 "pong"（健康检查）
POST /predict     → multipart/form-data 推理
```

### 2.1 `POST /predict` 请求格式

multipart 表单字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `entries` | 文本（JSON 字符串） | **PipelineRequest**，见下 |
| `image` | 二进制文件 | 图像输入（与 text 二选一） |
| `text` | 文本 | 文本输入（CLIP 文本编码） |

`PipelineRequest` 是嵌套字典 `{task: {type: {modelName, options?}}}`：

- task ∈ `"facial-recognition" | "clip" | "ocr"`（注意 CLIP 任务键是 `clip` 不是 search）
- type ∈ `"detection" | "recognition" | "textual" | "visual"`

服务端实际发送的三类请求（TS `MachineLearningRepository`）：

```jsonc
// CLIP 文本（encodeText，带翻译语言时才有 options）
{"clip":{"textual":{"modelName":"ViT-B-32__openai","options":{"language":"en"}}}}
// CLIP 图像（encodeImage）
{"clip":{"visual":{"modelName":"ViT-B-32__openai"}}}
// 人脸（detectFaces，detection+recognition 两段）
{"facial-recognition":{
  "detection":{"modelName":"buffalo_l","options":{"minScore":0.7}},
  "recognition":{"modelName":"buffalo_l"}}}
// OCR（两段）
{"ocr":{"detection":{"modelName":"PP-OCRv5_mobile","options":{"minScore":0.5,"maxResolution":736}},
        "recognition":{"modelName":"PP-OCRv5_mobile","options":{"minScore":0.8}}}}
```

### 2.2 响应格式

JSON 对象，键为 task 名 + 图像尺寸：

```jsonc
{"clip":"[0.123,-0.456,...]",            // 嵌入：orjson 序列化的 float32 数组字符串（512 维）
 "facial-recognition":[{"boundingBox":{"x1":..,"y1":..,"x2":..,"y2":..},"embedding":"[...]","score":0.93}],
 "ocr":{"text":[..],"box":[..],"boxScore":[..],"textScore":[..]},
 "imageHeight":1080,"imageWidth":1920}
```

> **关键细节**：embedding **不是 base64**。模型用 `orjson.dumps(arr, OPT_SERIALIZE_NUMPY)`
> 序列化 numpy 数组，得到的是 JSON 数字数组字符串（如 `"[0.5,0.5,0.7071]"`）。

### 2.3 可用性与故障转移

- 服务端按 `machineLearning.urls`（默认 `["http://immich-machine-learning:3003"]`）轮询；
- 周期性（默认 30s，超时 2s）`GET {url}/ping` 维护健康位图；
- predict 时先尝试所有健康实例再尝试不健康实例，成功即置健康、失败置不健康。

## 3. Go 移植映射表

| 上游组件 | immich-go 对应 | 兼容策略 |
|---|---|---|
| NestJS 控制器 + `/api` 前缀 | `internal/api`（chi 路由） | 相同方法+路径+状态码+DTO 字段（camelCase、RFC3339 毫秒） |
| Zod DTO | `internal/api/dto.go` 手写结构体 | 与 OpenAPI spec 字段一一对应 |
| bcrypt(10) + 不透明 token + SHA-256 落库 | `internal/crypto` + `internal/auth` | 算法/常量/头名/Cookie 名一致；API Key 同构 |
| Kysely + PostgreSQL | `internal/store/duckstore`（内嵌 DuckDB） | users/sessions/api_keys/assets(+exif)/albums(+有序资产/共享用户) 全部持久化于 `<media>/immich.duckdb`，与向量库共享连接（单轨后端） |
| **pgvector / VectorChord 向量检索** | **`internal/vectordb`（内嵌 DuckDB）** | `smart_search`/`face_search`/`person` 表 + SQL `array_cosine_similarity` 余弦检索 + Go 侧 DBSCAN 人脸聚类 + 并查集近重复检测；向量数据持久化于 `<media>/vectors.duckdb`，无需 PostgreSQL/Redis。详见 docs/duckdb-vectordb.md |
| BullMQ + Redis 19 队列 | `internal/jobs` 进程内队列 | 队列名/统计字段与 `/api/jobs` 完全一致 |
| `MachineLearningRepository` | `internal/ml` 客户端 | **wire 级兼容**：/ping、/predict multipart（entries/image/text）、pipeline JSON、失败转移、嵌入解码（JSON 数组字符串）逐字段断言测试 |
| sharp 缩略图 | `internal/media`（x/image CatmullRom） | thumbnail 250 / preview 1440 / JPEG q80；解码失败回退原图 |
| 上传流式落盘 + SHA-1 | `internal/storage` | 相同目录布局（upload/thumbs/<userId>/<a>/<b>/…）与去重语义 |
| pgvector 余弦检索 | 内嵌 DuckDB（`ml.CosineSimilarity` Go 侧兜底） | 语义一致，brute-force；HNSW 可经 DuckDB vss 扩展引入 |
| socket.io 事件 | 未移植 | — |
| Web 前端静态托管 | 未移植 | 需配合官方 web 静态产物自行反代 |

## 4. 已实现端点清单（全部挂 `/api`）

**公开**：`GET /server/ping`、`/server/version`、`/server/version-history`、`/server/media-types`、`POST /auth/login`、`POST /auth/admin-sign-up`

**认证后**：

- auth：`validateToken`、`logout`、`change-password`、`GET /auth/status`
- users：`GET /users`、`GET/PATCH /users/me`、`GET /users/{id}`
- api-keys：CRUD + `/me` + `/{id}/rotate`（创建返回一次性 `secret`）
- sessions：`GET /sessions`、`DELETE /sessions`、`DELETE /sessions/{id}`
- assets：`POST /assets`（multipart 上传 + SHA-1 查重 + `x-immich-checksum` 预查）、`GET /assets/{id}`、`PUT /assets/{id}`、`PUT/DELETE /assets`（批量）、`GET /assets/statistics`、`POST /assets/bulk-upload-check`、`POST /assets/jobs`、`GET /assets/{id}/original|thumbnail|video/playback`
- albums：列表/建/改/删、`PUT /albums/{id}/assets`、`DELETE /albums/{id}/assets`、`PUT /albums/assets`（批量）、`PUT /albums/{id}/users`、`DELETE /albums/{id}/user/{userId}`、`GET /albums/statistics`
- timeline：`GET /timeline/buckets`、`GET /timeline/bucket`（列式 DTO）
- search：`POST /search/metadata`、`POST /search/smart`（走 ML CLIP → DuckDB 余弦）
- 向量/聚类：`GET /people`（DuckDB DBSCAN 人物簇）、`GET /duplicates`（CLIP 近重复组）、`POST /jobs {"name":"face-clustering"|"detect-duplicates"}`（手动触发，immich-go 扩展）
- people 全套：创建/改名/收藏/隐藏/删除（单个+批量）、`POST /people/{id}/merge`（合并）、`PUT /people/{id}/reassign`（人脸重归属）、`GET /people/{id}/statistics`、`GET /people/{id}/thumbnail`（从原图按人脸框裁剪头像）
- memories 全套：列表/创建/统计/详情/更新/删除/资产增删
- sync **增量化**：`GET/POST/DELETE /sync/ack` + `POST /sync/stream`（NDJSON；全局 update_id 单调序列戳在每个实体行上，ack 为 `<Type>:<updateId>` 水位；流返回水位之后的实体 + 硬删除墓碑 AssetDeleteV1/AlbumDeleteV1/UserDeleteV1；支持 AuthUsersV1/UsersV1/AssetsV1/AssetsV2/AlbumsV1/AlbumsV2）
- 重复项处理：`POST /duplicates/resolve`（保留/回收分组内资产并解散组）、`DELETE /duplicates[/{id}]`
- 下载：`POST /download/info`（按 archiveSize 分卷预估）、`POST /download/archive`（zip 流式下载）
- 地图/文件夹：`GET /map/markers`（带 GPS 资产聚合点）、`GET /map/reverse-geocode`（无本地地理数据，返回空表优雅降级）、`GET /view/folder[/unique-paths]`
- Web 首启与偏好：`GET/PUT /users/me/preferences`（默认值深合并）、`GET/POST /system-metadata/admin-onboarding`、`GET /config[/defaults]`、`GET /public/config[/defaults]`、`GET /server/apk-links`、`GET /server/version-check`
- trash：`POST /trash/empty`、`POST /trash/restore`
- jobs：`GET /jobs`（19 队列统计）、`PUT /jobs/{name}`、`POST /jobs`
- server：`about`、`config`、`features`、`storage`、`statistics`

## 5. 尚未移植 / 后续路线

已按优先级完成 P0/P1 批次（覆盖 102/274 操作）：Web 首启（preferences/onboarding/config/public-config）、memories 全套、sync 基础版、people 管理（含合并/重归属/统计/头像裁剪）、duplicates resolve、download（info+zip 流）、map markers、view folder、apk-links、version-check。**设计性跳过：`/server/license*`（无需许可证）与 `/shared-links`（暂不实现）。**

剩余缺口：

1. **admin 域 32 操作**：用户管理（增删改/恢复/统计）、数据库备份、完整性报告、维护模式、通知模板——官方管理面板所需。
2. **资产长尾**：`/assets/copy`、资产级 metadata CRUD 与 edits（beta 编辑）、`/assets/{id}/ocr`、HLS 自适应流 4 端点（依赖 ffmpeg 转码）。
3. **用户长尾**：`PUT /users/me`、calendar-heatmap、license（跳过）、onboarding 3 端点、preferences（admin 侧 2 端点）、头像上传/读取。
4. **共享链接（9，按决策暂缓）**、**partners（5）**、**stacks（7）**、**tags（9）**、**libraries（8，外部目录扫描）**、**activities（4，相册评论）**、**faces（4）**、**asset-files（4）**、**notifications（6）**、**workflows（8）**、**cluster-groups（7）**、**plugins（4）**。
5. ~~sync 细粒度增量~~ ✅ 已完成：全局 update_id 序列 + 墓碑表 sync_deletes，Assets/Albums/Users 类型支持增量与删除事件（内存与 DuckDB 双后端）。
6. **search 长尾**：explore/places/cities/person/random/statistics/suggestions（8）。
7. **system-config（4）**、**system-metadata 其余 2**、**queues 新形态（5）**、**oauth（6）**、**socket.io 实时事件**、**多 worker 进程模型**。
