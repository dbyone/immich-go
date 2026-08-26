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
