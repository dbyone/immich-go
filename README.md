# immich-go

[![CI](https://github.com/dbyone/immich-go/actions/workflows/ci.yml/badge.svg)](https://github.com/dbyone/immich-go/actions/workflows/ci.yml)

Immich 服务端的 Go 语言实现，**保持对官方 `immich-machine-learning` 服务的接口兼容**，
API 面向 Immich v3.1.0（`/api` 前缀、相同路由、相同 DTO、相同鉴权模型）。

**向量数据库与聚类分析使用内嵌 DuckDB**（替代上游 pgvector/VectorChord + Redis）：
CLIP/人脸向量、SQL 余弦检索、DBSCAN 人脸聚类（自动生成人物）、CLIP 近重复检测，
全部内置于单个二进制。**实体元数据（用户/会话/API Key/资产/相册）同样持久化在
DuckDB**——`<media>/immich.duckdb` 一个文件即服务端的完整状态，重启不丢；无需
PostgreSQL 与 Redis（`IMMICH_STORE=memory` 可退回易失内存后端）。

> 注意：go-duckdb 需要 CGO —— 本机构建需安装 gcc/g++（Windows 用
> [WinLibs](https://winlibs.com/) / MSYS2），Docker 构建已内置（见 Dockerfile）。

## 快速开始

```bash
go build -o immich-go ./cmd/immich-go
IMMICH_PORT=2283 IMMICH_MEDIA_LOCATION=./data ./immich-go
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
internal/ml/            immich-machine-learning 客户端（wire 级兼容）★
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
| 向量库 | ✅ 内嵌 DuckDB（smart_search/face_search/person 表、SQL 余弦、DBSCAN 人物聚类、近重复检测），无需 PostgreSQL/Redis |
| 实体持久化 | ✅ 用户/会话/API Key/资产(含 EXIF)/相册/回忆/同步ack/系统元数据 均持久化于 `<media>/immich.duckdb`，重启不丢（已实测重启恢复） |
| EXIF 元数据 | ✅ 纯 Go 解析 JPEG/TIFF（相机/镜头、原始拍摄时间、GPS 坐标、描述、评分、方向修正宽高），无 exiftool 依赖 |
| 视频元数据 | ✅ 纯 Go 解析 MP4/MOV（时长、分辨率、fps、编码、旋转；ffprobe 处理其他容器）；缩略图经 ffmpeg 抽帧生成海报（无 ffmpeg 时优雅降级） |
| Immich API | ✅ 102/274 操作（37%）：auth/assets/albums/timeline/trash/search/jobs/queues-legacy/sessions/api-keys/users 基础/server(无 license)/people 全套/memories 全套/sync 基础版/duplicates 全套/download/map/view/config/public-config/onboarding 均可用；**shared-links 暂不实现**（设计决策） |
| 官方 Web / 移动端 | ⚠️ 首启向导、偏好、回忆、人物管理、重复项处理、下载、地图、文件夹视图已可用；admin 面板、OAuth、伙伴、堆叠、标签、共享链接等未覆盖 |

详见 [docs/architecture-analysis.md](docs/architecture-analysis.md)（上游架构分析 + 移植映射 + 路线图）、
[docs/ml-interface.md](docs/ml-interface.md)（ML 接口兼容矩阵）与
[docs/duckdb-vectordb.md](docs/duckdb-vectordb.md)（DuckDB 向量库与聚类分析）。

## 测试

```bash
go test ./...        # 单元 + 端到端（含伪造 ML 服务的 wire 格式断言）
go vet ./...
```

## License

[MIT](LICENSE)

本项目是基于对上游 Immich（AGPL-3.0）公开 API 行为的独立分析编写的兼容实现，
未复制其源代码；本仓库自身按 MIT 授权发布。
