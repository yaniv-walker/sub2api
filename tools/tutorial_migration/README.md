# 教程归档与草稿导入

Python 3.10+，只使用标准库。所有导入都保存为草稿，不会发布；目标站内容和素材属于不可信输入，后端仍会执行格式校验和 HTML 清洗。

## 1. 生成本地归档

```powershell
python tools/tutorial_migration/migrate.py snapshot `
  --source https://api.codelink.chat `
  --archive E:\tutorial-archives\codelink-20261105
```

可选：`--source-json <已保存的列表响应.json>` 复用先前采集的正文；`--proxy http://127.0.0.1:7897` 使用本机代理。需要源站鉴权时，进程环境变量 `TUTORIAL_SOURCE_TOKEN` 提供 Bearer，禁止把令牌写入命令、仓库或归档。可用 `--source-token-env` 指定另一变量名。

归档目录包含：

- `manifest.json`：13篇文档/所有素材的完整文件 SHA-256、字节数、来源、采集时间；只有所有文件完成后生成。
- `documents/`：原始文档，包括标题、正文、分类、排序、下载资源和派生关系。
- `media/`：完整图片和视频，以哈希命名；源站地址无扩展名时按实际 MIME 恢复扩展名。
- `download-progress.json`：失败后重跑时复用的已校验文件索引。
- `review.json`：域名、售后联系方式、价格及截图/视频中的品牌信息审校清单。

同一快照不覆盖已有不同正文；源站变化时创建新归档目录。网络错误可再次执行同一命令继续下载。不会使用 HEAD 判定资源是否存在，也不会将分段样本当作完整文件。

```powershell
python tools/tutorial_migration/migrate.py verify --archive E:\tutorial-archives\codelink-20261105
```

## 2. 预览与导入

通过安全方式将本站管理员令牌提供给进程环境变量 `TUTORIAL_TARGET_TOKEN`。下面命令不包含令牌：

```powershell
# 只读检查：显示新增/已存在/slug冲突；不会上传或写文章
python tools/tutorial_migration/migrate.py import `
  --target http://127.0.0.1:18088 `
  --archive E:\tutorial-archives\codelink-20261105

# 本地/测试站确认清单后导入，仍全部是草稿
python tools/tutorial_migration/migrate.py import `
  --target http://127.0.0.1:18088 `
  --archive E:\tutorial-archives\codelink-20261105 `
  --profile tools/tutorial_migration/relaygateway-profile.json --apply
```

外部目标只允许 HTTPS；HTTP 只用于 loopback。客户端限制请求为本站相对路径，拒绝跨站重定向，防止携带管理员凭证访问另一站。

以源站 origin + 原始文档 ID 识别已导入内容；slug 冲突整批停止。已完成文章再次导入时跳过，即使后来人工修改过，也不会覆盖。中断恢复只接续版本号与导入日志一致的未完成草稿。素材按原始地址全批复用；不会每篇重新上传80个资源。

归档旁边的 `import-<目标origin哈希>.json` 是本次批次日志，包含文档 ID、版本号与素材映射，无认证信息。日志也应保留，以便断点恢复和回滚。若请求返回前网络断开，先在后台核对新建文章，工具不会未经核实自动接管可能人工编辑过的源站同名记录。

## 3. 审核与发布

不提供 `--profile` 时导入稿保留原文。指定已确认的本站档案后，会替换站名、API域名、客服块，去除原站价格与“绝不封号”等承诺；不自动替本站决定具体价格、充值规则或模型分组。原始归档始终保留，原站二维码也保存在素材库中，但适配后的正文不引用它。逐篇在后台编辑：

1. 将源站域名改为本站域名，按具体客户端核验是否保留 `/v1` 等后缀。
2. 替换客服账号、二维码、服务时间及商业条款。
3. 重截图/重录画面内含源站信息的资源，或在稿中说明其仅作界面示例。
4. 校验下载链接与标签一致，正文图片视频均走本站媒体地址。
5. 预览、保存、发布；验证管理员、普通用户与匿名分享三种身份。

13篇派生文档导入后独立可编辑，保留来源关系；修改完整版不会自动覆盖其他分篇。

## 4. 回滚批次

```powershell
# 默认只读列出将删除的草稿
python tools/tutorial_migration/migrate.py rollback `
  --target http://127.0.0.1:18088 `
  --journal E:\tutorial-archives\codelink-20261105\import-XXXXXXXXXXXX.json
```

添加 `--apply` 才执行。只处理该批次创建且版本未变、仍为草稿的文档；发现已编辑或已发布则整批拒绝。物理素材保留，避免误删共享或历史版本资源，后续通过素材引用检查后回收。

首次改造本地数据库前，应保存数据库快照和原素材目录。上线使用已有生产发布/备份流程；本工具不会部署程序或连接生产数据库。

## 验证

```powershell
python -m unittest discover -s tools/tutorial_migration -v
```

完整归档应额外用图片解码器检查所有图片、浏览器播放视频；哈希验证确认归档完整性，不证明教程内容与本站业务规则一致。
