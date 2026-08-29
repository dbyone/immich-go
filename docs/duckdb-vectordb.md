# DuckDB：向量库、聚类分析与实体持久化（替代 pgvector/VectorChord + PostgreSQL + Redis）

immich-go 不依赖 PostgreSQL + pgvector/VectorChord，而是把**全部持久化状态**内嵌到服务进程：
`internal/vectordb`（向量与聚类）与 `internal/store/duckstore`（实体元数据）共用同一个
DuckDB 数据库文件（默认 `<media>/immich.duckdb`），单文件承载完整服务端状态，零外部服务。

## 1. 与上游方案的对照

| 能力 | 上游（pgvector / VectorChord） | immich-go（DuckDB） |
|---|---|---|
| CLIP 嵌入存储 | `smart_search.embedding vector(512)` + HNSW `vector_cosine_ops` | `smart_search.embedding FLOAT[DIM]` |
| 人脸嵌入存储 | `face_search.embedding vector(512)` | `face_search.embedding FLOAT[DIM]` |
| 智能搜索 | `<=>` 余弦距离 + `set local vchordrq.probes` | SQL `array_cosine_similarity(...)` ORDER BY（brute-force） |
| 人脸聚类（people） | 服务端聚类（facialRecognition 队列） | 同语义的 DBSCAN（Go 实现，数据来自 DuckDB） |
| 重复检测 | `duplicateDetection` 队列 + pgvector 近邻 | SQL 自连接余弦 + 并查集分组 |
| 实体元数据 | PostgreSQL + Kysely（user/session/api_key/asset/album...） | `duckstore`：users/sessions/api_keys/assets(+asset_exifs)/albums(+album_assets/album_users) |
| 部署依赖 | PostgreSQL ≥14 + pgvector/VectorChord 扩展 + Redis | 无（单二进制内嵌） |

> DuckDB 侧 SQL 余弦不可用时（如裁剪版构建）自动降级为 Go 侧余弦排序，
> 行为一致（`Store.HasSQLCosine()` 在启动日志中标注）。

## 2. 表结构

**向量库（`internal/vectordb`）**

```sql
smart_search(asset_id PK, owner_id, model, embedding FLOAT[DIM], updated_at)
face_search(asset_id, face_idx, owner_id, person_id, x1..y2,
            embedding FLOAT[DIM], updated_at, PRIMARY KEY(asset_id, face_idx))
person(id PK, owner_id, name, is_hidden, is_favorite,
       face_count, thumbnail_asset_id, created_at, updated_at)
```

**实体元数据（`internal/store/duckstore`）**

```sql
users(id PK, email UNIQUE, password(bcrypt), name, is_admin, should_change_password,
      avatar_color, profile_image_path, storage_label, is_onboarded,
      created_at, updated_at, deleted_at)
sessions(id PK, token_hash BLOB, user_id, device_os, device_type, app_version,
         created_at, updated_at, expires_at)          -- 会话 token 的 SHA-256
api_keys(id PK, name, key_hash BLOB, user_id, permissions, created_at, updated_at)
assets(id PK, owner_id, type, original_path, thumbnail_path, preview_path,
       original_file_name, original_mime_type,
       file_created_at, file_modified_at, local_datetime, created_at, updated_at, deleted_at,
       is_favorite, duration, checksum BLOB, checksum_b64, width, height, visibility,
       library_id, live_photo_video_id, duplicate_id, thumbhash)
       -- (owner_id, checksum) 索引支撑上传去重
asset_exifs(asset_id PK, make, model, lens_model, file_size, exif_width, exif_height,
            date_time_original, latitude, longitude, city, state, country, description, rating)
albums(id PK, owner_id, album_name, description, album_thumbnail_asset_id,
       created_at, updated_at, deleted_at, is_activity_enabled, sort_order)
album_assets(album_id, asset_id, position, PK(album_id, asset_id))  -- position 保持资产顺序
album_users(album_id, user_id, role, PK(album_id, user_id))
```

- `DIM` 由配置决定（`IMMICH_VECTOR_DIM`，默认 512，须与 ML 模型输出一致）；
- 向量写入用 `TRY_CAST('[0.1,0.2,...]' AS FLOAT[DIM])` 字符串绑定，读回 `CAST(embedding AS VARCHAR)`；
- API Key 权限以逗号拼接存 VARCHAR（权限名不含逗号）；
- 所有数据（实体 + 向量 + 人物）在单文件中持久化，重启不丢（有专门的重开持久化测试）；
- 实体与向量共享单个数据库连接（串行），避免 DuckDB 嵌入式单写者的并发事务冲突。

## 3. 聚类分析

### 3.1 人脸聚类 → 人物（people）

上游 facial-recognition 流程的语义：对每个用户的人脸嵌入做基于密度的聚类，
密度达到阈值的人群成为 person，孤立人脸不归属任何人。immich-go 用标准
DBSCAN（`vectordb.DBSCAN`）等价实现：

- 距离度量：余弦距离 `1 − cos(a,b)`；
- `eps` = `IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MAX_DISTANCE`（默认 0.5，同上游）；
- `minPts` = `..._MIN_FACES`（默认 3，同上游 minFaces）；
- 重聚类时沿用旧 person id（人群身份跨运行稳定），空且未命名的 person 自动清理。

触发方式：
1. 人脸检测作业完成后防抖 5s 自动调度（`IMMICH_CLUSTER_DEBOUNCE_MS`）；
2. 手动：`POST /api/jobs {"name":"face-clustering"}`。

查询：`GET /api/people` → `[{id, name, faceCount, thumbnailAssetId, isHidden, isFavorite}]`。

### 3.2 CLIP 近重复检测

`smart_search` 上的 SQL 自连接找出余弦距离 < `IMMICH_MACHINE_LEARNING_DUPLICATE_DETECTION_MAX_DISTANCE`
（默认 0.01，同上游 maxDistance）的资产对，并查集聚成组后把 `duplicateId`
写回资产。`GET /api/duplicates` 返回 `[{id, duplicateCount, assets:[...]}]`。

触发方式与聚类一致（嵌入写入后防抖 / `POST /api/jobs {"name":"detect-duplicates"}`）。

## 4. 复杂度与规模说明

- 检索/聚类均为暴力扫描：DuckDB 列式 `FLOAT[DIM]` 全表余弦，10 万级向量
  内单查询毫秒~几十毫秒量级，个人库（万级）绰绰有余；
- 若需要 ANN（HNSW）索引，可在 DuckDB 中 `INSTALL vss; LOAD vss;` 后建
  HNSW 索引，代码无需改动（预留：`HasSQLCosine` 探测机制可扩展为探测索引）；
- 聚类的邻域计算 O(n²)，在 Go 侧单核执行，人脸规模（千~万级）可接受。

## 5. 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `IMMICH_DUCKDB` | `<media>/immich.duckdb` | DuckDB 数据库文件（实体 + 向量） |
| `IMMICH_VECTOR_DB` | — | 兼容别名（旧版仅向量文件，现指向同一数据库） |
| `IMMICH_VECTOR_DIM` | `512` | 向量维度（须匹配模型） |
| `IMMICH_CLUSTER_DEBOUNCE_MS` | `5000` | 聚类/去重防抖窗口 |
| `IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MAX_DISTANCE` | `0.5` | DBSCAN eps |
| `IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MIN_FACES` | `3` | DBSCAN minPts |
| `IMMICH_MACHINE_LEARNING_DUPLICATE_DETECTION_ENABLED` | `true` | 近重复检测开关 |
| `IMMICH_MACHINE_LEARNING_DUPLICATE_DETECTION_MAX_DISTANCE` | `0.01` | 重复判定距离 |

## 6. 构建说明

go-duckdb 需要 CGO（C++ 编译 DuckDB amalgamation）：

- Linux/Docker：`apk add gcc g++ musl-dev`（见 Dockerfile），`CGO_ENABLED=1`；
- Windows：安装 WinLibs/MSYS2 的 gcc/g++ 后 `CGO_ENABLED=1 go build`；
- 首次编译 DuckDB amalgamation 需数分钟，属正常现象。
