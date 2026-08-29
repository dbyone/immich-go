# plan.md — MT Photos 与 Immich 差异分析及借鉴要点

> 调研日期：2026-08-26。证据来源：
> - MT Photos 官方演示站 <https://d.mtmt.tech/>（V1.56.0，浏览器实测：demo 账号全功能走查）
> - 上游 Immich 仓库 `web/` 前端源码（本地 `.immich-ref/immich`，路由树与 asset-viewer 组件取证）
> - MT Photos 官方文档 <https://mtmt.tech/docs/>、开放 API 文档 <https://mtmt.tech/api/>、AI 边车开源仓库 <https://github.com/MT-Photos/mt-photos-ai>
> - 上游 Immich `server/src/repositories/machine-learning.repository.ts`（ML wire 协议参照）

---

## 1. 结论速览

| 维度 | Immich | MT Photos |
| --- | --- | --- |
| 设计哲学 | 照片库优先（上传导入，服务器管理存储） | 文件系统优先（不搬运原文件，目录即相册） |
| AI 策略 | 自带 ML 容器（immich-machine-learning 一体化） | 外接 AI API（GPS / 人脸 / CLIP / LLM 可自由组合） |
| 商业模式 | MIT 开源，自托管免费 | 闭源，license 订阅制 |
| 前端技术 | Svelte/SvelteKit | Vue + Vuetify |
| 后端 API | OpenAPI 3 公开规范，生态客户端丰富 | 自有协议，Swagger 需连实例浏览，配套自家 App |
| 后台 API 兼容性 | — | **与 Immich 完全不兼容**（见 §5） |

## 2. 页面 / 功能对照

### 2.1 MT Photos（演示站实测）

| 模块 | 实测要点 |
| --- | --- |
| 照片流 | 日期分组 + **年份快轨**（右侧竖向时间轴快速跳年）+ 缩放滑杆 |
| 照片查看器 | 信息面板含备注 / 星级 / 大模型识别结果 / **场景分类标签（带置信度）**；可直接修改拍摄时间与位置 |
| 探索 | 人物 / 地点 / 场景 / 标签，另有 RAW / 全景 / 大文件 / **年度报告**等特殊类别 |
| 文件夹 | 文件系统目录树直接浏览（文件系统优先的直接体现） |
| 分享 | 分享给我的 / 我分享的（双向视图） |
| 相似照片 | **MD5 精确重复过滤** + 批量清理 |
| 高级搜索 | 6 种搜索类型（含以文搜图，走 Chinese-CLIP） |
| 系统配置 | 外接 AI API 管理、30+ 维护任务、**应用内升级**、订阅管理 |

### 2.2 Immich（web/ 源码路由树）

`photos / albums / archive / favorites / folders / locked / map / memories / partners / people / places / recently-added / search / shared-links / sharing / tags / trash / user-settings / admin`

asset-viewer 组件含 OcrBoundingBox（OCR 高亮）、ActivityViewer（照片内评论/点赞）、StarRating 等。

### 2.3 双方对照差异

| 能力 | Immich | MT Photos | 备注 |
| --- | --- | --- | --- |
| 年份快轨 | ✗ | ✓ | Immich 时间轴仅头部月份分组 |
| 场景分类标签 | ✗（仅人物/地点） | ✓（LLM/VLM 打标带置信度） | Immich 需自建 job 或靠搜索 |
| 年度报告 | ✗ | ✓ | |
| MD5 精确去重 UI | ✓（近重复+精确，算法内建） | ✓（显式 MD5 过滤开关） | |
| 文件名/路径搜索 | 部分（有 file name 搜索） | ✓ | |
| 地图 | ✓ | ✓（外接高德/Mapbox） | |
| 回忆 Memories | ✓ | ✗（无独立入口） | |
| 共享伙伴 Partners | ✓ | ✗（走分享体系） | |
| 公网分享链接 | ✓（shared-links，含过期/密码） | ✓（分享体系） | immich-go P1 范围 |
| 回收站/归档/收藏 | ✓ | ✓ | 品类标配 |
| 照片内活动（评论/赞） | ✓ | ✗ | |
| OCR 高亮 | ✓ | ✓（PaddleOCR） | |
| 应用内升级 | ✗（依赖容器/包管理） | ✓ | |
| 多用户 | ✓ | ✓（订阅按用户数计费） | |

## 3. 三大核心差异

1. **设计哲学：文件系统优先 vs 照片库优先。** MT Photos 面向 NAS 用户，只索引不搬运，目录即相册，删除操作默认不动原文件；Immich 是"上传即入库"，服务器掌管存储与备份。这决定了两者 API 形态（MT 围绕路径，Immich 围绕 asset）与运维心智完全不同。
2. **AI 策略：外接组合 vs 自带容器。** MT Photos 把 GPS 逆地理（高德/Mapbox）、人脸识别、CLIP、LLM 描述全部做成可插拔的外接 API，用户按需组合，甚至可自部署开源边车 [mt-photos-ai](https://github.com/MT-Photos/mt-photos-ai)（PaddleOCR + Chinese-CLIP）；Immich 把 ML 打包进官方 immich-machine-learning 容器，开箱即用但难以替换。
3. **商业模式：闭源订阅 vs 开源免费。** MT Photos 按 license/订阅收费且服务端闭源；Immich MIT 开源。本仓库（immich-go）正是后者的 Go 重写，MIT 授权。

## 4. immich-go 可借鉴点（候选路线图）

> 2026-08-26 更新：2/3/4/5/6 已全部落实（见 §7）。
> 2026-08-28 更新：官方 Web 前端（v3.1.0 fork）已内置于本仓库并由 Go embed 托管，
> 前端侧增强（单图刷新按钮、仅完全重复开关）已随 fork 落实；年份快轨经源码核实
> **上游本就内置**（Scrubber.svelte：竖向年份标签+月份刻度+拖动导航，hover 展开），
> 此前"Immich 无年份快轨"的判断有误，无需重造。

1. ~~**年份快轨**~~：上游已内置（Scrubber.svelte），此前判断有误。
2. ✅ **单图刷新按钮**：前端 DetailPanel 刷新按钮 → `POST /api/assets/{id}/refresh` → 重载资产。
3. ✅ **MD5 精确重复过滤开关**：前端重复项页"仅完全重复"开关 → `GET /api/duplicates?exact=true`（按 SHA-1 分组）；顺带修复 `/api/duplicates` 响应契约漂移（`duplicateId`/`suggestedKeepAssetIds` 对齐上游 DuplicateResponseDto）。
4. ✅ **文件名/路径搜索**：smart search 先做文件名/路径精确匹配再并入向量结果（无 ML 也可用）；metadata 搜索新增 `originalFileName`/`originalPath` 过滤。
5. ✅ **场景分类标签**：`internal/classify` 零样本 CLIP 打标 + `场景/<标签>` 层级标签 + `GET /api/assets/{id}/classification` 实时得分端点；官方 Web 的 Tags 页与资产详情标签面板自动展示。
6. ✅ 应用内升级检查：暂不做（依赖发布渠道，低价值）。

### 4.1 可插拔 AI（超出原计划的落地）

`internal/ml.Provider` 抽象 + mt-photos-ai 方言适配器（`IMMICH_MACHINE_LEARNING_PROVIDER=mtphotos`），
wire 契约依据边车源码逐条验证（`api-key` 头、200-带-msg 错误包络、字符串化浮点/OCR box、
无人脸端点→作业自动跳过）。见 docs/ml-interface.md。

### 4.2 文件夹功能（用户点名需求）

`GET /api/view/folder*` 修正为上游精确语义（`path` 参数、直接子项匹配、按文件名排序、
timeline 可见性过滤），官方 Web 的 Folders 页直接可用——MT 的"文件系统优先"浏览模式
在 Immich 客户端内即插即用。配套 tags 全套 CRUD（134/274 操作，48.9%）支撑场景标签展示。

## 5. 后台 API 兼容性评估：**不兼容**

用户问题：MT Photos 与 Immich 界面风格很像，其后台 API 是否兼容？——**不兼容，且无任何互操作性。** 风格相似只因同属自托管相册品类（网格流、深色主题、信息面板都是品类惯例）；MT Photos 用 Vue+Vuetify，Immich 用 Svelte，连前端栈都不同。

| 维度 | Immich | MT Photos |
| --- | --- | --- |
| 规范 | OpenAPI 3 公开规范（immich-openapi-specs 仓库），274 端点 | 自有协议；官方 Swagger（mtmt.tech/api）需连接实例浏览，未发布离线规范 |
| 鉴权 | session cookie / bearer / `x-api-key`（API Key） | `MT-API-AUTH-KEY` header（token 于管理后台生成） |
| 路径语义 | `/api/*`，围绕 asset/album/person 等资源 | 自有路径，围绕其文件系统优先模型 |
| ML 契约 | `GET /ping`→`pong`；`POST /predict` multipart（`entries` 携带 task→type→{modelName,options} 流水线 JSON + `image`/`text` 文件字段），embedding 为 JSON 数字数组字符串 | 边车 mt-photos-ai 自有端点，`API_AUTH_KEY` 环境变量验证请求来源；PaddleOCR/Chinese-CLIP 模型体系不同 |
| 客户端生态 | 官方 App/CLI + 大量第三方（上传器、壁纸、电视端等） | 仅自家 iOS/Android App 与 Web |

具体依据：

- Immich 服务端 ML 调用见上游 `machine-learning.repository.ts`：`fetch(new URL('predict', url), {method:'POST', body: formData})`，formData 含 `entries`（JSON.stringify 的任务配置）与 `image`/`text` 字段，健康检查打 `ping`，多 URL 健康优先故障转移——**immich-go 的 `internal/ml/client.go` 已在 wire 级复刻此契约**（有测试钉死）。
- MT Photos 的 AI 边车鉴权是"容器内部用来验证请求来源"的 `API_AUTH_KEY`（官方文档原话，示例 `mt_photos_ai_extra`），与 Immich 的无鉴权 ML 内网契约不同；其开放 API 鉴权为 `MT-API-AUTH-KEY`。
- 两者鉴权头、路径、请求/响应模型、ML 边车协议均无交集，故：Immich 客户端连不上 MT Photos 服务端，反之亦然。

对 immich-go 的含义：**无需也无法兼容 MT 协议**；唯一有意义的交集是"迁移工具"（读取 MT Photos 索引库导出元数据再经 Immich API 导入），属于独立工具范畴，不在服务端路线图内。

## 6. 后续动作

- [x] §4-2/3/4 后端部分 + §4-5 场景分类适配层（2026-08-26 完成，含 mt-photos-ai Provider）
- [x] 文件夹功能（上游精确语义，官方 Web /folders 可用）
- [x] tags 全套（CRUD/bulk/层级，覆盖 134/274 = 48.9%）
- [x] 官方 Web 前端内置（2026-08-28）：`web/` fork 自 v3.1.0 + Go embed 单二进制托管；
      前端落实单图刷新按钮与"仅完全重复"开关；重复项 API 契约漂移修复
- [x] 2026-08-28 review 批修复：tags 重复创建 409 / 改名级联子标签 / bulk 无效 id 400；
      folders O(n²)→sort.Slice；smart search archived 过滤（无 ML 空结果改 200）；
      /trash/restore/assets、/search/suggestions 枚举契约修正（camera-make 等）；
      router 历史重复注册块清理；TagDeleteV1 墓碑移除（改为 bump 关联资产）；
      webui 按 Accept-Encoding 直发 .br/.gz（-79% 传输）；覆盖 130/254 = 51.2%
- [x] socket.io（2026-08-29）：采用 zishang520/socket.io v3.0.4（Socket.IO v4+/EIO4 wire
      协议，活跃维护，MIT）——internal/realtime 网关：握手鉴权与 REST 同链、按用户/会话进房、
      连接即发 on_server_version；上传/更新/软删/恢复/硬删/批量操作全部接入实时广播；
      wire 级测试（其官方 client 模块回环验证握手+事件+拒绝路径）与真机冒烟双通过
- [x] sync 流补齐（2026-08-29）：AssetExifV1（含 Partner/Album 变体）随资产水位增量下发，
      MemoryV1/MemoryAssetV1/MemoryDeleteV1 墓碑（memories 表补 update_id 戳与写入）；
      官方 App 的离线 EXIF/回忆缓存缺口闭合（types: assetExifsV1/memoriesV1/memoryToAssetsV1）
- [ ] 场景词表扩展为可配置（外置 taxonomy 文件或 LLM/VLM 外接分类器）
- [ ] 本文档随版本演进维护，重大结论变更需注明日期
