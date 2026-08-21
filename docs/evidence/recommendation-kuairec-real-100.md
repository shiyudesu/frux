# KuaiRec 真实离线评估证据

## 数据来源与完整性

- 官方来源：[KuaiRec Zenodo record 18164998](https://zenodo.org/records/18164998)
- 项目来源：[KuaiRec 官方仓库](https://github.com/chongminggao/KuaiRec)
- License：CC BY-SA 4.0
- 下载文件：`KuaiRec.zip`
- 官方 MD5：`261550d472c48eff4990fb13c0e5bcf7`
- 下载后 MD5：`261550d472c48eff4990fb13c0e5bcf7`

原始文件保存在 Git 忽略目录 `apps/api/.offline-data/kuairec/`，不进入仓库。

## 确定性选择规则

第一次真实运行使用 Small Matrix 中数值最小的 100 个 `user_id`：

- 原始选中交互：331,424；
- 官方数据中缺少 `time/date/timestamp`、不能用于 chronological split 的记录：12,005；
- 实际评估交互：319,419；
- 视频：3,327；
- 有效 Session Case：100/100；
- Neutral 交互：132,695；
- Missing watch ratio：0。

高于 100 的 watch ratio 被保留，因为它与 `play_duration / video_duration` 一致，代表重复播放；
评估器当前允许有限值 `[0,1000]`。缺少时间的记录不会被伪造成最早或最晚事件。

## Provenance

- Release：`2.0-small-matrix-smallest-100-users-timestamp-present`
- Manifest SHA-256：`9872a23f23a8b8a1194c375dd7519e61c9441b9c609fb4d13df3a5785c61ca8e`
- Interactions SHA-256：`7f1d24b14171e384c92ef36ef7bf9c65438ee5f8eb9bdfe48edd3a01f2a9975b`
- Categories SHA-256：`9223312e437e65f6773627f5665d9cdf5cb99d70df1fe08c1baa1a609a9094e0`
- Canonical JSON report SHA-256：`58936868a0b728ee5914698706e0da7bfb47e60eb8fee4895ae7ee2c6f22d6f2`
- Markdown report SHA-256：`cecd0d806bced574e55840f8f4dfb7125726451f52df580022cae28484c86963`
- External model calls：0

两次独立执行的 JSON 与 Markdown 均逐字节一致。

## 结果

| Baseline | HitRate@1 | HitRate@5 | HitRate@10 | HitRate@20 | MRR | Catalog Coverage |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Popularity | 0.03 | 0.14 | 0.21 | 0.38 | 0.107239 | 0.272017 |
| Recent Interaction | 0.33 | 0.63 | 0.73 | 0.84 | 0.472181 | 0.166216 |
| Category | 0.02 | 0.07 | 0.11 | 0.21 | 0.068211 | 0.206492 |
| Text | unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |
| Image | unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |
| Multimodal | unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |
| Multimodal + Session | unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |

Recent Interaction 在该 chronological protocol 下明显强于 Popularity，但覆盖面较低。Category
单独使用时弱于 Popularity，且 Top-20 primary-category repeated runs 为 1,608，说明分类排序高度集中。

内容 Baseline unavailable 是正确结果：官方包没有与当前 manifest 兼容的预计算 Text/Image/
Multimodal 向量。评估器没有补零、调用模型或把 Caption/Category 冒充向量。

## 解释边界

- 这是确定性的 100 用户真实子集，不是全量 Small Matrix 结论；
- 结果是 offline/non-causal，不代表 Frux 线上提升；
- Recent Interaction 的优势可能包含数据收集时间结构，不等于可直接上线；
- 在补充兼容的预计算内容特征前，不能比较 Session Semantic 与内容 Baseline；
- 报告不会自动推荐策略、进入 Shadow 或 Rollout。
