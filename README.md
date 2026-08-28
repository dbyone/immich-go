# immich-go

[![CI](https://github.com/dbyone/immich-go/actions/workflows/ci.yml/badge.svg)](https://github.com/dbyone/immich-go/actions/workflows/ci.yml)

Immich 服务端的 Go 语言实现，**保持对官方 `immich-machine-learning` 服务的接口兼容**，
API 面向 Immich v3.1.0（`/api` 前缀、相同路由、相同 DTO、相同鉴权模型）。

**AI 服务可插拔**：默认对接 `immich-machine-learning`（`/ping` + `/predict` wire 契约），
一行环境变量即可切换为 **mt-photos-ai 边车**（`IMMICH_MACHINE_LEARNING_PROVIDER=mtphotos`，
Chinese-CLIP + PaddleOCR，`api-key` 鉴权）——见下文[可插拔 AI](#可插拔-ai外部模型服务)。
内置 **MT Photos 风格场景分类**：零样本 CLIP 打标，自动生成 `场景/<标签>` 层级标签。

**向量数据库与聚类分析使用内嵌 DuckDB**（替代上游 pgvector/VectorChord + Redis）：
CLIP/人脸向量、SQL 余弦检索、DBSCAN 人脸聚类（自动生成人物）、CLIP 近重复检测，
全部内置于单个二进制。**实体元数据（用户/会话/API Key/资产/相册/标签）同样持久化在
DuckDB**——`<media>/immich.duckdb` 一个文件即服务端的完整状态，重启不丢；无需
PostgreSQL 与 Redis（`IMMICH_STORE=memory` 可退回易失内存后端）。

**官方 Web 前端已内置**：`web/` 目录 fork 自上游 Immich v3.1.0 的 SvelteKit 应用
（adapter-static），编译产物经 Go `embed` 打进同一个二进制——启动后浏览器直接访问
`http://localhost:2283` 即是完整 Web 界面（照片流/相册/人物/地图/文件夹/标签/重复项…），
无需单独部署 nginx 或 web 容器；深链由 SPA fallback 托底，`/api` 保持 JSON 语义。
fork 中落实了 MT Photos 借鉴增强：**照片详情面板单图刷新按钮**（重跑该资产的
EXIF/缩略图/CLIP/场景标签流水线）、**重复项页"仅完全重复"开关**（SHA-1 字节级分组），
并修正了 `/api/duplicates` 响应与上游 DuplicateResponseDto 的契约漂移
（`duplicateId`/`suggestedKeepAssetIds`）。注意 `web/` 与 `i18n/` 源自上游 AGPL-3.0
代码，沿用该授权（见 web/LICENSE 与 web/README.md 的 fork 说明）；仓库其余部分为 MIT。

> 注意：go-duckdb 需要 CGO —— 本机构建需安装 gcc/g++（Windows 用
> [WinLibs](https://winlibs.com/) / MSYS2），Docker 构建已内置（见 Dockerfile）。
> 前端产物已随仓库提交，日常 `go build` 无需 Node；改前端后进入 `web/` 执行
> `corepack pnpm install && corepack pnpm run build` 重建（需 Node 24 + pnpm 10）。

## 快速开始

```bash
go build -o immich-go ./cmd/immich-go
IMMICH_PORT=2283 IMMICH_MEDIA_LOCATION=./data ./immich-go
# 浏览器打开 http://localhost:2283 —— 内置 Web 界面即开箱可用
```

或使用 Docker Compose（含官方 machine-learning 容器）：

```bash
docker compose up
```

首次使用：

```bash
# 1. 注册管理员
curl -X POST localhost:2283/api/auth/admin-sign-up \
  -H 'Content-Type: application/json' \
  -d '{"name":"Admin","email":"admin@example.com","password":"password"}'

# 2. 登录（返回 accessToken，也可用 cookie）
curl -X POST localhost:2283/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"password"}'

# 3. 上传照片（SHA-1 自动去重）
curl -X POST localhost:2283/api/assets \
  -H "Authorization: Bearer $TOKEN" \
  -F assetData=@photo.jpg \
  -F fileCreatedAt=2026-08-24T10:00:00.000Z \
  -F fileModifiedAt=2026-08-24T10:00:00.000Z

# 4. 智能搜索（文本 → CLIP → 余弦排序）
curl -X POST localhost:2283/api/search/smart \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"query":"sunset on the beach"}'
```

## 鉴权（与官方一致）

- 会话：`Authorization: Bearer <token>`、`x-immich-user-token`、Cookie `immich_access_token`
- API Key：`x-api-key`（创建时返回一次性 `secret`，服务端只存 SHA-256）
- 密码 bcrypt(cost 10)；登录返回 **201** 并种 Cookie

## 可插拔 AI（外部模型服务）

`IMMICH_MACHINE_LEARNING_PROVIDER` 选择 AI 方言，两者实现同一个 Provider 接口：

| | `immich`（默认） | `mtphotos` |
|---|---|---|
| 服务 | [immich-machine-learning](https://github.com/immich-app/immich/tree/main/machine-learning) 容器 | [mt-photos-ai](https://github.com/MT-Photos/mt-photos-ai) 边车（开源） |
| 契约 | `GET /ping`→`pong`、`POST /predict` multipart（`entries` 流水线 JSON + `image`/`text`） | `POST /check`、`/clip/img`、`/clip/txt`、`/ocr`（multipart `file` / JSON body） |
| 鉴权 | 无（内网） | `api-key` 头（值 = 边车的 `API_AUTH_KEY`） |
| 模型 | ViT-B-32 CLIP、buffalo_l 人脸、PP-OCR | Chinese-CLIP ViT-B-16、RapidOCR |
| 能力 | CLIP + 人脸 + OCR | CLIP + OCR（**无人脸**：人脸作业自动跳过） |

```bash
# 对接 mt-photos-ai（docker run -d -p 8060:8060 -e API_AUTH_KEY=xxx mtphotos/mt-photos-ai）
IMMICH_MACHINE_LEARNING_PROVIDER=mtphotos \
IMMICH_MACHINE_LEARNING_URL=http://<host>:8060 \
IMMICH_MACHINE_LEARNING_API_KEY=xxx \
./immich-go
```

> 注意：切换 Provider 后已入库的向量语义不同，需重新触发 smart-search
> （对资产 `POST /api/assets/{id}/refresh` 或重新上传）。

### 场景分类（MT Photos 风格，默认关闭）

```bash
IMMICH_SCENE_CLASSIFICATION_ENABLED=true     # 开关
IMMICH_SCENE_CLASSIFICATION_THRESHOLD=0.24   # 余弦阈值（mtphotos 方言默认 0.30）
IMMICH_SCENE_CLASSIFICATION_TOP_K=3          # 每资产最多标签数
```

开启后，smart-search 作业入库向量时顺手做零样本打标：内置约 90 个中英双语场景
词表（海滩/美食/婚礼/屏幕截图/夜景……），immich 方言嵌入英文提示词、mtphotos
方言嵌入中文（Chinese-CLIP 原生），命中的标签自动写入层级标签 `场景/<中文>`
（Immich Web 的 Tags 页与 metadata 搜索 `tagIds` 过滤即可消费）。

### immich-go 扩展端点（官方客户端不依赖，工具可用）

| 端点 | 作用 |
|---|---|
| `POST /api/assets/{id}/refresh` | 重跑该资产的元数据流水线（EXIF→缩略图→CLIP→人脸→场景分类） |
| `GET /api/assets/{id}/classification` | 实时零样本场景得分（读取已存向量，`[{label,score}]`） |
| `GET /api/duplicates?exact=true` | 只看字节级完全重复（按 SHA-1 分组，MT 的 MD5 过滤同款） |

另有 MT 借鉴增强：smart search 在向量检索前先做**文件名/路径精确匹配**（无 ML
服务时也能搜文件）；文件夹视图 `GET /api/view/folder*` 已按上游精确语义实现
（`path` 参数、直接子项、按文件名排序、timeline 可见性过滤），官方 Web 的
Folders 页可直接使用。

## 项目结构

```
cmd/immich-go/          入口
internal/config/        环境变量配置（与上游同名）
internal/domain/        实体（对齐上游表结构语义）
internal/store/         持久化接口
internal/store/duckstore/ DuckDB 实体存储（users/sessions/api_keys/assets/albums）★
internal/store/memory/  内存后端（IMMICH_STORE=memory）
internal/vectordb/      DuckDB 向量库（CLIP/人脸向量、SQL 余弦检索、DBSCAN 聚类、近重复检测）★
internal/auth/          会话/API Key 鉴权（与上游 AuthService.validate 同优先级）
internal/ml/            AI Provider 层：immich-machine-learning 客户端（wire 级兼容）+ mt-photos-ai 适配器 ★
internal/classify/      零样本场景分类（中英双语词表、CLIP 零额外推理打标）
internal/api/           REST handlers（DTO 对齐 OpenAPI spec）
internal/jobs/          进程内作业队列（19 个队列名与 /api/jobs 一致）
internal/media/         缩略图生成（图像 250/1440 + ffmpeg 视频抽帧）
internal/exif/          纯 Go EXIF 解析（IFD0/ExifIFD/GPS：相机、拍摄时间、坐标、评分、方向）
internal/videometa/     纯 Go MP4/MOV 元数据解析（时长/分辨率/fps/编码/旋转）+ ffprobe 回退
internal/storage/       磁盘布局（upload/thumbs/<userId>/<a>/<b>/...）
internal/app/           装配 + 后台作业流水线（元数据→缩略图→CLIP→人脸→聚类/去重）
docs/                   架构分析、ML 兼容、DuckDB 向量库文档
```

## 与官方组件的兼容关系

| 组件 | 状态 |
|---|---|
| immich-machine-learning（容器） | ✅ 直接对接（`/ping`、`/predict` multipart、多实例故障转移） |
| mt-photos-ai（边车） | ✅ 直接对接（`/clip/img`、`/clip/txt`、`/ocr`、`api-key` 鉴权、CLIP+OCR；人脸不可用时作业自动跳过） |
| 向量库 | ✅ 内嵌 DuckDB（smart_search/face_search/person 表、SQL 余弦、DBSCAN 人物聚类、近重复检测），无需 PostgreSQL/Redis |
| 实体持久化 | ✅ 用户/会话/API Key/资产(含 EXIF)/相册/回忆/标签/同步ack/系统元数据 均持久化于 `<media>/immich.duckdb`，重启不丢（已实测重启恢复） |
| EXIF 元数据 | ✅ 纯 Go 解析 JPEG/TIFF（相机/镜头、原始拍摄时间、GPS 坐标、描述、评分、方向修正宽高），无 exiftool 依赖 |
| 视频元数据 | ✅ 纯 Go 解析 MP4/MOV（时长、分辨率、fps、编码、旋转；ffprobe 处理其他容器）；缩略图经 ffmpeg 抽帧生成海报（无 ffmpeg 时优雅降级） |
| Immich API | ✅ 130/254 操作（51.2%，对 v3.1.0 基准）：auth/assets/albums/timeline/trash/search 全套(8 个长尾端点)/jobs/sessions 全套/api-keys/users 基础/server(无 license)/people 全套/memories 全套/**tags 全套（CRUD/bulk/层级）**/**view folder（上游精确语义）**/**sync 增量化（update_id 水位 + 删除事件）**/duplicates/download/map/stacks 全套/partners 全套/config/public-config/onboarding 均可用；**shared-links 暂不实现**（设计决策） |
| 官方 Web / 移动端 | ⚠️ 首启向导、偏好、回忆、人物管理、重复项处理、下载、地图、**文件夹视图**、**标签页**（含场景分类标签）已可用；admin 面板、OAuth、伙伴、堆叠、共享链接等未覆盖 |

详见 [docs/architecture-analysis.md](docs/architecture-analysis.md)（上游架构分析 + 移植映射 + 路线图）、
[docs/ml-interface.md](docs/ml-interface.md)（ML 接口兼容矩阵）与
[docs/duckdb-vectordb.md](docs/duckdb-vectordb.md)（DuckDB 向量库与聚类分析）。

## 测试

```bash
go test ./...        # 单元 + 端到端（含伪造 ML 服务的 wire 格式断言）
go vet ./...
```

## License

- **`web/` 与 `i18n/`**：fork 自上游 Immich 的 AGPL-3.0 代码（含本地增强，
  清单见 [web/README.md](web/README.md) 与 [web/LICENSE](web/LICENSE)），
  按原授权发布；`web/build/` 编译产物同源同授权。
- **仓库其余部分**：[MIT](LICENSE)。基于对上游 Immich 公开 API 行为的独立分析
  编写的兼容实现，未复制其服务端源代码。
