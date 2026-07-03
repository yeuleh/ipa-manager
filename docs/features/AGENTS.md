# docs/features — per-mission 规格文档

每个子目录 = 一个 mission（feature 工作流单元），含完整的 MindTrek feature workflow 产物。这些文档是**单一事实源**（requirements/design 已验收，改前先读）。

## 目录布局（每个 mission）

```
<mission-slug>/
  requirements.md    需求（user stories / AC / NFR）—— 单一事实源
  design.md          设计（架构 / DD 决策 / 接口 / 流程）
  plan.md            任务拆解（垂直切片 / 依赖图 / traceability / Minor ledger）
  e2e_test.md        E2E case（从 AC 单向派生 / traceability matrix）
  validate.md        验收证据包（validate 阶段，部分 mission）
```

## 工作流阶段

requirements → design → plan → execution → validate → dock。每阶段经 Spock 评审 + 用户验收才推进；phase instructions 由系统注入。

## ⚠️ Live Amendment 模式（重要）

当 execution/validate 阶段**真机实证推翻**了 requirements/design 的前提，在该文档**顶部加 Live Amendment banner**（权威更正，覆盖下文冲突陈述），而非全文重写。下文保留作历史记录，但 amendment 为准。

实例：`ios-device-manage/` —— live 实测 iOS 26 install 无需 tunnel，推翻原 US-07 前提，tunnel 机器移除（requirements/design/e2e_test 各加 Live Amendment）。

## 关键约定

- spec 是事实源：实现不偏离；若需偏离，**先更新 spec**（regress）+ 重新 Spock + 用户验收。
- 历史文献结论（如 `docs/bootstrap/research.md`）可能过时——**实证 > 文献**，live 实测是金标准。
- mission slug 永久不可改。
