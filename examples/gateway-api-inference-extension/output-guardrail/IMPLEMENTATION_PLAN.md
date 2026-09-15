# Istio Gateway LLM Guard PoC - 实施计划 & 进度跟踪

**创建日期**: 2026-09-15  
**状态**: 🟢 In Progress  
**所有者**: YiqiWang

---

## Executive Summary

### 问题
**老板的问题**: Study if we can hook llm-guard in Istio gateway using existing Istio plugins, do PoC if possible.

### 答案
✅ **Yes. We can hook llm-guard in Istio Gateway.**

We are now implementing a production-grade PoC using 4 staged PRs over 2-3 weeks.

### 核心方案
- 在 Istio Gateway 的 HTTP filter chain 中插入 Envoy `ext_proc` filter
- 调用独立的 gRPC service（llm-guard-ext-proc）检测和修改 LLM 输出
- 不改动 KAITO Controller，保持现有系统独立性
- 复用现有的 OutputGuardrails 库（已在 RAGEngine 中使用）

---

## 进度跟踪 & PR 里程碑

| PR | 标题 | 目标 | 代码量 | 状态 | ETA |
|----|------|------|--------|------|-----|
| **PR1** | Minimal ext_proc gRPC | 验证"能否拦截和修改响应" | ~120 LOC | 🟢 IN PROGRESS | This week |
| **PR2** | OpenAI JSON adapter | 验证"能否正确处理 JSON" | ~120 LOC | 🔵 PLANNED | +3 days |
| **PR3** | Reuse OutputGuardrails | 验证"能否接入实际检测" | ~150 LOC | 🔵 PLANNED | +5 days |
| **PR4** | E2E tests + docs | 验证"整个系统工作" | 0 new LOC | 🔵 PLANNED | +7 days |

**总耗时**: 2-3 周（单人开发）

---

## 详细进度

### 🟢 PR1: Minimal ext_proc gRPC Server

**目标**: 证明 Istio → ext_proc → response mutation 链路能通

**做什么**:
- 最小 gRPC server（ExternalProcessor service）
- 接收 response_body，追加 `[GATEWAY_TEST]` 字符串
- 返回修改后的 body 给 Envoy
- 不涉及 llm-guard、JSON 解析、任何业务逻辑

**交付物**:
- `processor/server.py` (120 LOC)
- `envoyfilter.yaml` (Istio 配置)
- `deployment.yaml` (K8s 部署)
- `Dockerfile` (容器镜像)

**验收标准** ✅:
- [ ] ext_proc service 启动成功
- [ ] EnvoyFilter 加载无错误
- [ ] 客户端收到响应末尾有 `[GATEWAY_TEST]`
- [ ] 现有的 EPP routing 不被破坏

**当前状态**: 代码已完成，等待集群部署验证

---

### 🔵 PR2: OpenAI JSON Response Adapter

**目标**: 证明能正确解析和修改 OpenAI 格式 JSON

**做什么**:
- 从字节流解析 JSON
- 提取 `choices[*].message.content`
- 进行修改（暂时还是追加测试字符串）
- 重新序列化并返回
- 处理 malformed JSON / 非 JSON 响应

**代码**:
```python
def parse_and_modify(body_bytes):
    response_json = json.loads(body_bytes)
    for choice in response_json.get('choices', []):
        choice['message']['content'] += " [MODIFIED]"
    return json.dumps(response_json, ensure_ascii=False).encode('utf-8')
```

**验收标准**:
- [ ] 有效的 OpenAI JSON 被正确解析和重构
- [ ] `choices[0].message.content` 被修改
- [ ] 其他字段保持不变
- [ ] 非 JSON 响应被 skip

**ETA**: PR1 验证后 3-5 天

---

### 🔵 PR3: Reuse Existing OutputGuardrails

**目标**: 验证能接入现有的 guardrails 库，实现实际的 redact/block

**做什么**:
- 从 `presets/ragengine/guardrails/` 导入 `GuardrailsReloader` 和 `OutputGuardrails`
- 在 ext_proc 中调用 `guardrails.guard_response()`
- 支持现有的 scanner：ban_substrings、regex、secrets、PII
- 支持 redact 和 block 动作

**代码复用**:
```python
from presets.ragengine.guardrails import GuardrailsReloader

guardrails_reloader = GuardrailsReloader(policy_path='/etc/llm-guard/policy.yaml')
guardrails = guardrails_reloader.get_current()
guarded_response = guardrails.guard_response(response_obj, request_metadata={})
```

**验收标准**:
- [ ] OutputGuardrails 初始化成功
- [ ] Policy 从 ConfigMap 正确加载
- [ ] ban_substrings scanner 触发时内容被 redact
- [ ] block action 触发时整个消息被替换为 blockMessage
- [ ] Fail-closed 行为正确

**ETA**: PR2 验证后 1-2 天

---

### 🔵 PR4: E2E Tests + Documentation

**目标**: 验证整个系统端到端工作，并完整文档化

**做什么**:
- EnvoyFilter + Deployment 的完整配置
- E2E 测试脚本（clean / redact / block / fail scenarios）
- EPP routing 回归测试（确保原有功能不破坏）
- Latency benchmark（记录基线数据）
- 完整 README 和 architecture 文档
- Known limitations 清单

**测试场景**:
```bash
# 正常情况（无触发）
curl ... → response 原样返回

# Redact 场景
curl ... (输出包含 INTERNAL_SECRET) 
→ [REDACTED]

# Block 场景
curl ... (输出包含 PROHIBITED)
→ "Response blocked by output guardrails."

# Fail scenario
kill ext_proc service
→ curl 失败或超时（fail-closed 确认）

# Regression
ab -n 100 ...
→ 验证请求分散到多个 pod，latency 符合预期
```

**验收标准**:
- [ ] 所有测试场景通过
- [ ] EPP routing 未被破坏
- [ ] Latency 基线数据完整
- [ ] README 清楚说明限制和后续
- [ ] 六个总体完成标准都✓

**ETA**: PR3 验证后 2-3 天

---

## 总体完成标准（六个✓）

```
① Istio Gateway 成功加载额外 ext_proc filter
   验证: istioctl proxy-config listener | grep ext_proc
   ✓

② non-streaming response body 成功进入 processor
   验证: 日志看到 Process() 被调用
   ✓

③ Clean output 原样返回
   验证: 无 scanners 触发，响应内容未变
   ✓

④ Unsafe output 可以 redact
   验证: ban_substrings 触发，content = [REDACTED]
   ✓

⑤ Unsafe output 可以 block
   验证: 触发 block，content = blockMessage
   ✓

⑥ 加 guard 后原有 KAITO GWIE/EPP routing 正常
   验证: 请求分散到多个 pod，latency 相近
   ✓
```

**这六个都✓ = PoC 成功 = 问题回答：Yes, we can do it.**

---

## 产品价值路线 (L1/L2/L3)

### L1: Non-Streaming Output Guard (当前 PoC - 2-3 周)
✅ **完成后立即可用** → 所有 `stream=false` 的推理请求都能被守护

**应用场景**:
- Chat completions (non-streaming)
- Batch inference
- Offline inference
- Tool result validation

**技术**:
- Envoy `response_body_mode: BUFFERED`
- 完整 JSON 一次性处理

---

### L2: Streaming Deterministic Guard (可选后续 - +1-2 周)
✅ **显著提升产品价值** → 支持 `stream=true` 的实时聊天

**应用场景**:
- Real-time chat with token streaming
- Latency < 1s TTFT

**技术**:
- Envoy `response_body_mode: FULL_DUPLEX_STREAMED`
- 复用 RAGEngine 现有 SSE parser + holdback buffer
- Deterministic scanners only (ban_substrings, secrets, PII)

**代码复用**:
- ✅ `presets/ragengine/streaming/guardrails.py` 已有 holdback logic
- ✅ `DEFAULT_STREAMING_GUARDRAILS_HOLDBACK_LEN = 256` 已有设计

---

### L3: Model-Based Streaming Scanners (研究任务 - 需评估成本)
🔬 **高价值但需要研究** → 支持 Toxicity / Relevance / FactualConsistency

**前置工作**:
- 在 RAGEngine 应用层测：CPU/GPU overhead、延迟影响、准确度
- 决策：< 10% overhead 才可做 Gateway L3

**如果评估结果可行**:
- 在 FULL_DUPLEX_STREAMED 中加入 model-based scanning
- 需要 semantic chunk boundary 设计

---

## 后续选择 (PoC 成功后)

### 选项 A: 快速发布 L1
```
PoC L1 完成
  ↓
Phase 4: 集成到 KAITO API
  ↓
发布：用户可通过 InferenceSet spec 声明 gatewayGuardrails
```
**时间**: PoC 完成 + 2 周集成
**价值**: 立即可用的 non-streaming guard

---

### 选项 B: 完整发布 L1+L2 (推荐)
```
PoC L1 完成
  ↓
L2: Streaming + deterministic scanners
  ↓
Phase 4: 集成到 KAITO API
  ↓
发布：完整的流式和非流式 guard
```
**时间**: PoC + 2 周 L2 + 2 周集成 = 6-7 周总耗时
**价值**: 支持现代 LLM streaming 场景，产品竞争力高

---

### 选项 C: 最终愿景 L1+L2+L3
```
PoC L1 完成
  ↓
L2: Streaming deterministic
  ↓
[并行] RAGEngine L3 成本评估
  ↓
[如果可行] L3: Model-based scanners
  ↓
Phase 4: 集成到 KAITO API
  ↓
发布：Production-grade 完整 gateway guardrail
```
**时间**: PoC + 2 周 L2 + 2 周 L3 研究 + 3 周实现 + 2 周集成 = 3-4 月
**价值**: 业界最完整的 gateway 级 LLM output guardrail

---

## 风险评估

| 风险 | 严重度 | 缓解措施 |
|------|--------|---------|
| Istio EnvoyFilter 插入破坏 EPP | 🟠 中 | Phase 1 就验证，isolated 不改 Controller |
| gRPC streaming 协议实现错误 | 🟡 低 | Envoy 文档明确，有 PoC 代码参考 |
| Response mutation 导致 frame 错误 | 🟡 低 | Phase 4 有 curl -v 完整验证 |
| OutputGuardrails 集成失败 | 🟢 很低 | 代码已有，只是导入和调用 |
| 性能开销过大 | 🟡 低 | Phase 4 有 benchmark，可接受 +30-70% overhead |

**总体**: 🟢 **低风险** - 每个 PR 都独立可验证，出问题时影响范围最小

---

## 工程质量指标

| 指标 | 目标 |
|------|------|
| 代码行数 | PR1-3 总计 < 400 LOC（都是新增，无重复） |
| 复用代码 | 60%+ 来自现有 OutputGuardrails / streaming guardrails |
| 改动范围 | 0 行改动现有 Controller / API / RAGEngine runtime |
| 测试覆盖 | PR4 包含 clean / redact / block / fail 4 个场景 |
| 文档 | README + architecture + known limitations |

---

## 时间表详细版

```
Week 1 (now):
├─ Mon-Tue: PR1 验证 (ext_proc 链路)
├─ Wed-Thu: PR2 验证 (JSON 处理)
└─ Fri: PR3 初始化 (guardrails 集成)

Week 2:
├─ Mon-Tue: PR3 测试 (redact/block 场景)
├─ Wed: PR4 E2E 测试
├─ Thu: Benchmark + 文档
└─ Fri: 所有 ✓ 完成，ready for review

总耗时: 10 个工作日 = 2 周（保守估计）
```

---

## 关键交付物清单

### 代码
- ✅ `processor/server.py` - gRPC server
- ✅ `envoyfilter.yaml` - Istio filter 配置
- ✅ `deployment.yaml` - K8s deployment
- ✅ `Dockerfile` - 容器镜像定义

### 测试
- ✅ E2E 测试脚本
- ✅ EPP routing 回归测试
- ✅ Latency benchmark

### 文档
- ✅ README (部署和测试说明)
- ✅ ARCHITECTURE.md (设计文档)
- ✅ LIMITATIONS.md (已知限制和后续方向)
- ✅ 本文档 (IMPLEMENTATION_PLAN.md)

---

## 信息更新频率

- **每个 PR 完成**: 更新进度状态 (这份文档的"进度跟踪"表)
- **每周 Friday**: 汇总周进度
- **有阻挡**: 即时通知

---

## 联系方式

有任何问题或需要修改方向，随时联系。

**YiqiWang**  
分支: `feat/istio-llm-guard-pr1`  
计划文档: `examples/gateway-api-inference-extension/output-guardrail/IMPLEMENTATION_PLAN.md`
