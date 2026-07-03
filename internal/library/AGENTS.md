# internal/library — per-profile 本地 IPA 库

按 profile 隔离的本地 `.ipa` 文件存储 + 元数据索引。library 是 install 的快速缓存层（library 有→直接推送；无→`device install` 自动下载再推）。

## 物理布局

```
<libraryRoot>/<profileID>/
  index.json          元数据索引（atomic write）
  <bundle-id>_<app-id>_<version>.ipa
```
每个 profile 独立目录（多账号隔离）。

## Store 接口

`Store`（CLI 经 `Deps.LibraryStore` 注入）：`Add` / `List` / `Get` / `GetVersion` / `Remove` / `RemoveVersion` / `CleanAll`。

## 关键约定

- **复合键**：entry 由 `(bundle_id, version)` 复合键唯一（同一 app 多版本共存）。`Add` 同 bundle+version 覆盖，不同 version 追加。CLI 默认 install 取**最近下载**（`mostRecentByDownloadedAt`，按时间戳，非语义版本比较——避免不可靠的 version-string 比较）；`--version <v>` 指定。
- **原子写**：`index.json` 经 tmp + rename 原子写（避免半写损坏）。
- **per-profile 隔离**：任一命令不得读写非目标 profile 的目录（NFR-03 of download mission）。
- library 本身不含凭据（IPA 文件本机用户已可见；iTunesMetadata.plist 含 email 是 Apple 固有行为，可接受）。
