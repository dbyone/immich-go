# immich-machine-learning 接口兼容说明

immich-go 的机器学习客户端（`internal/ml`）与官方 `immich-machine-learning`
容器（immich_ml FastAPI 服务）**wire 级兼容**：请求字节、响应解析、健康检查与
故障转移行为均对齐上游 TypeScript `MachineLearningRepository`
（`server/src/repositories/machine-learning.repository.ts`）。

## 兼容矩阵

| 能力 | 上游行为 | immich-go | 验证 |
|---|---|---|---|
| 健康检查 | 周期 `GET {url}/ping`，200 即健康 | 相同（默认 30s / 2s 超时） | `TestPing` |
| 多实例故障转移 | 健康实例优先，失败降级标记不健康 | 相同 | `TestFailoverToSecondURL` |
| CLIP 文本编码 | `entries` 携带 `{"clip":{"textual":{...}}}` + `text` 字段 | 相同（JSON 逐字节断言） | `TestEncodeTextWireFormat` |
| CLIP 图像编码 | `{"clip":{"visual":{...}}}` + `image` 文件 | 相同 | `TestEncodeImageWireFormat` |
| 人脸检测/识别 | `facial-recognition` 下 detection+recognition 两段 | 相同 | `TestDetectFacesWireFormat` |
| OCR | `ocr` 下两段（minScore/maxResolution） | 相同 | — |
| 嵌入解码 | orjson 数字数组字符串（非 base64） | `DecodeVector` 解析 JSON 数组 → []float32 | `TestPipelineRequestJSON` |
| 相似度 | pgvector 余弦 (`<=>`) | `CosineSimilarity` | `TestCosineSimilarity` |

## 环境变量（与上游同名）

| 变量 | 默认 | 说明 |
|---|---|---|
| `IMMICH_MACHINE_LEARNING_ENABLED` | `true` | 总开关 |
| `IMMICH_MACHINE_LEARNING_URL` | `http://immich-machine-learning:3003` | 服务地址 |
| `IMMICH_MACHINE_LEARNING_URLS` | — | 逗号分隔多地址（故障转移） |
| `IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK` | `true` | 周期健康检查 |
| `IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK_TIMEOUT` | `2000` | ping 超时（ms） |
| `IMMICH_MACHINE_LEARNING_AVAILABILITY_CHECK_INTERVAL` | `30000` | ping 间隔（ms） |
| `IMMICH_MACHINE_LEARNING_CLIP_MODEL` | `ViT-B-32__openai` | CLIP 模型 |
| `IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MODEL` | `buffalo_l` | 人脸模型 |
| `IMMICH_MACHINE_LEARNING_FACIAL_RECOGNITION_MIN_SCORE` | `0.7` | 检测阈值 |
| `IMMICH_MACHINE_LEARNING_OCR_*` | 见 config | OCR 参数 |

## 在流水线中的位置

上传 → `metadataExtraction` → `thumbnailGeneration`
                          ↘ `smartSearch`（EncodeImage → 资产嵌入）
                          ↘ `faceDetection`（DetectFaces → 人脸框与嵌入）

`POST /api/search/smart {"query":"..."}` → `EncodeText` → 余弦排序 → 响应。

嵌入与人脸向量持久化在内嵌 DuckDB 向量库（`internal/vectordb`），
检测/编码完成后自动防抖触发 **DBSCAN 人脸聚类**（生成 `/api/people` 的人物）
与 **CLIP 近重复检测**（`/api/duplicates`），也可通过
`POST /api/jobs {"name":"face-clustering"}` / `{"name":"detect-duplicates"}` 手动触发。

## 与真实服务联调

```yaml
# docker-compose.yml 已内置官方 immich-machine-learning 镜像
services:
  immich-go:
    environment:
      IMMICH_MACHINE_LEARNING_URL: http://immich-machine-learning:3003
  immich-machine-learning:
    image: ghcr.io/immich-app/immich-machine-learning:release
```

模型权重在 ML 容器首次推理时自动下载；两容器无需任何代码耦合。

---

## 可插拔 Provider：mt-photos-ai 方言

`IMMICH_MACHINE_LEARNING_PROVIDER=mtphotos` 将 AI 后端切换为开源边车
[mt-photos-ai](https://github.com/MT-Photos/mt-photos-ai)（Chinese-CLIP ViT-B-16 +
RapidOCR）。两个方言实现同一个 `internal/ml.Provider` 接口，作业流水线无感知。

### wire 契约（依据其 onnx/server.py 源码验证）

| 端点 | 请求 | 响应 |
|---|---|---|
| `POST /check` | 仅 `api-key` 头 | `{"result":"pass",...}`；密钥错误 401 |
| `POST /clip/img` | multipart `file` 字段 | `{"result":["0.33..."]}`（16 位小数字符串数组） |
| `POST /clip/txt` | JSON `{"text":"..."}` | 同上 |
| `POST /ocr` | multipart `file` 字段 | `{"result":{"texts":[],"scores":[],"boxes":[{x,y,width,height}]}}`（全字符串标量） |

关键实现细节：

- **鉴权头是 `api-key`**（FastAPI `Header(...)` 把 `api_key` 参数映射为该名），
  值 = 边车容器的 `API_AUTH_KEY` 环境变量（immich-go 侧配
  `IMMICH_MACHINE_LEARNING_API_KEY`）。
- **失败也返回 HTTP 200**：`{"result":[],"msg":"..."}` —— 空 result 且带 msg
  即为失败，适配器将其还原为错误（`decodeMTVector`/`mtEnvelope`）。
- **没有人脸端点**：`DetectFaces` 返回 `ErrUnsupported`，上传流水线自动跳过
  人脸作业（`SupportsFaces()=false`），人物聚类对 mtphotos 方言不可用。
- 图片超 10000px 边车返回 `result:[]` + msg，同样按错误处理。

### 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `IMMICH_MACHINE_LEARNING_PROVIDER` | `immich` | `immich` 或 `mtphotos` |
| `IMMICH_MACHINE_LEARNING_API_KEY` | — | mtphotos 方言的 `API_AUTH_KEY` |

> 切换方言后向量语义不同，需对存量资产重新触发 smart-search
> （`POST /api/assets/{id}/refresh` 扩展端点）。

## 场景分类（零样本打标）

`internal/classify` 以 `IMMICH_SCENE_CLASSIFICATION_ENABLED=true` 启用：
smart-search 作业写入向量后顺手对比内置中英双语场景词表（约 90 类），
余弦 ≥ 阈值（immich 0.24 / mtphotos 0.30，可配）的前 TopK 个标签写入
层级标签 `场景/<中文>`；immich 方言嵌入英文提示词、mtphotos 方言嵌入中文
（Chinese-CLIP 原生优势）。词表嵌入每进程仅做一次并缓存，无逐资产额外推理。
