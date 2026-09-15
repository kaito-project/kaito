Subject: Istio Gateway LLM Guard PoC - 方案确认和进度更新

---

Hi [老板名],

## 核心问题的答案

**问题**: Study if we can hook llm-guard in Istio gateway using existing Istio plugins, do PoC if possible.

**答案**: ✅ **Yes, we can.** 我们现在在实施一个工程化的 PoC。

---

## 为什么能做

1. **技术可行性高**
   - Envoy 原生支持 ext_proc（external processing filter）
   - 可以在 Istio Gateway 的 filter chain 中插入自定义 gRPC service
   - 现有的 OutputGuardrails 库完全可以复用

2. **不影响现有系统**
   - 不改 KAITO Controller
   - 不改 InferenceSet / InferencePool
   - 不影响 EPP 的路由逻辑
   - 完全隔离，可随时添加或移除

3. **工程方案清晰**
   - 分 4 个小阶段，每阶段都有独立的验证
   - 代码量小（< 400 LOC 新增）
   - 复用现有代码 > 60%

---

## 实施计划（4个PR，2-3周）

| PR | 目标 | 进度 |
|----|------|------|
| PR1 | 验证"能否拦截和修改响应" | 🟢 这周完成 |
| PR2 | 验证"能否正确处理JSON" | 🔵 1周内 |
| PR3 | 验证"能否接入检测器" | 🔵 2周内 |
| PR4 | 完整E2E测试+文档 | 🔵 2-3周完成 |

**每个PR完成就有可演示的价值**（不是等到最后）

---

## 成功标准（六个✓）

```
✓ Istio Gateway 加载 ext_proc filter
✓ Response body 进入 processor
✓ Clean output 原样返回
✓ Unsafe output 可以 redact
✓ Unsafe output 可以 block
✓ EPP routing 未被破坏
```

六个都✓ = PoC 成功 = 问题被回答

---

## 产品价值路线

### L1 (2-3周) - 立即可用
- Non-streaming output guard
- 所有 `stream=false` 的推理都能被守护
- 可发布使用

### L2 (可选, +1-2周) - 显著提升
- Streaming 支持
- Real-time chat 也能被守护
- 产品竞争力提升

### L3 (研究中) - 高级功能
- Model-based 检测器（Toxicity, Relevance 等）
- 需先评估性能成本

---

## 风险评估

🟢 **低风险**
- 每个 PR 都独立验证
- 改动隔离（不涉及 Controller）
- 出问题时影响范围最小
- 可随时 rollback

---

## 后续行动

### 现在（这周）
- PR1 在进行中（ext_proc 链路验证）
- 等待集群部署验证

### 完成 PR1-4 后
- 选择：快速发布 L1，或等 L2，或等完整 L3

### 如果发布
- Phase 4: 集成到 KAITO API
- 用户可通过 InferenceSet spec 声明 gatewayGuardrails

---

## 详细计划文档

完整的实施计划、时间表、测试场景、工程指标都在：
```
examples/gateway-api-inference-extension/output-guardrail/IMPLEMENTATION_PLAN.md
```

---

## 下一步

- 任何问题随时反馈
- 每周 Friday 汇总进度
- 有阻挡立即通知

Thanks,
YiqiWang
